package matrix

// ─── Story 5-26: POST /_matrix/client/v3/user_directory/search ───────────────
//
// ATDD Red Phase — ALL tests in this file FAIL until user_directory.go exists
// and implements UserDirectoryHandler, EscapeLIKE, and the UserDirectoryDB
// interface.
//
// Bugs being fixed (see Story 5-26):
//   1. SearchTerm with LIKE metacharacters (%, _) causes full-table scans —
//      user enumeration vulnerability.
//   2. uid[1:strings.Index(uid,":")] panics when uid contains no ':' —
//      latent crash on malformed internal data.
//   3. Limit is clamped at 50 instead of 100; default is always 10 even when
//      Limit==0 case is not clearly distinguished from a valid small limit.
//
// Test strategy:
//   - EscapeLIKE is a pure function — no mocks needed. Tested directly.
//   - UserDirectoryHandler takes a UserDirectoryDB interface. A
//     mockUserDirectoryDB captures the escaped pattern passed to it and
//     returns controlled results — no real PostgreSQL required.
//   - buildAuthedUserDirectoryHandler wires JWTMiddleware → UserDirectoryHandler
//     exactly as main.go will do, so all auth paths are exercised.
//   - The NoPanic test passes a uid-without-colon via the mock and asserts
//     the handler returns 200 (row skipped, no panic).
//
// Types/functions that MUST be defined in user_directory.go for compilation:
//   - type UserDirectoryDB interface
//   - type UserDirectoryResult struct
//   - type UserDirectoryConfig struct
//   - type UserDirectoryHandler struct
//   - func NewUserDirectoryHandler(UserDirectoryConfig) *UserDirectoryHandler
//   - func (h *UserDirectoryHandler) Search(http.ResponseWriter, *http.Request)
//   - func EscapeLIKE(s string) string

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Mock UserDirectoryDB ─────────────────────────────────────────────────────

// mockUserDirectoryDB implements UserDirectoryDB.
//
// capturedPattern records the escaped LIKE pattern passed to SearchUsers so
// tests can assert the handler built the correct pattern before forwarding to
// the DB layer.
//
// rows is the slice of UserDirectoryResult the mock returns to the handler.
// queryErr, if non-nil, is returned instead of rows.
type mockUserDirectoryDB struct {
	capturedPattern string
	capturedLimit   int
	rows            []UserDirectoryResult
	queryErr        error
}

func (m *mockUserDirectoryDB) SearchUsers(ctx context.Context, pattern string, limit int) ([]UserDirectoryResult, error) {
	m.capturedPattern = pattern
	m.capturedLimit = limit
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.rows, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildAuthedUserDirectoryHandler wires JWTMiddleware → UserDirectoryHandler.Search
// on a dedicated ServeMux, mirroring how main.go will register the endpoint.
//
// JWT sub is always "test-sub-123" → authenticated user_id "@test-sub-123:test.local".
func buildAuthedUserDirectoryHandler(t *testing.T, db *mockUserDirectoryDB) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewUserDirectoryHandler(UserDirectoryConfig{
		DB:         db,
		ServerName: "test.local",
	})

	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	mux := http.NewServeMux()
	mux.Handle("POST /user_directory/search",
		jwtMiddleware(http.HandlerFunc(handler.Search)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), map[string]any{
			"name": "searchtest",
		})
	}

	return mux, makeToken
}

// postUserDirectorySearch is a small helper that fires a POST request and returns
// the recorder so tests can assert status code and body.
func postUserDirectorySearch(t *testing.T, handler http.Handler, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest("POST", "/user_directory/search", &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// ─── Unit Tests: EscapeLIKE (pure function, no DB, no HTTP) ──────────────────

// TestUserDirectory_EscapesPercentUnderscore (AC #2)
//
// EscapeLIKE must replace:
//   - '\' → '\\'  (backslash first — avoids double-escaping)
//   - '%' → '\%'
//   - '_' → '\_'
//
// Checked: the escaped pattern is correct AND wrapping (%…%) is not done
// inside EscapeLIKE (the handler wraps separately).
func TestUserDirectory_EscapesPercentUnderscore(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{input: "alice%test", want: `alice\%test`},
		{input: "alice_test", want: `alice\_test`},
		{input: `alice\test`, want: `alice\\test`},
		{input: "alice%_test", want: `alice\%\_test`},
		{input: `al\ice%_`, want: `al\\ice\%\_`},
		{input: "plain", want: "plain"},
	}
	for _, tc := range cases {
		got := EscapeLIKE(tc.input)
		if got != tc.want {
			t.Errorf("EscapeLIKE(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestUserDirectory_RejectsEmpty (AC #1: empty after trim → 400)
//
// SearchTerm "" and "   " must both produce 400 M_INVALID_PARAM.
// The mock DB must NOT be called (validation rejects before touching the DB).
func TestUserDirectory_RejectsEmpty(t *testing.T) {
	db := &mockUserDirectoryDB{}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	cases := []string{"", "   ", "\t"}
	for _, term := range cases {
		body := map[string]any{"search_term": term}
		w := postUserDirectorySearch(t, handler, token, body)

		if w.Code != http.StatusBadRequest {
			t.Errorf("search_term=%q: expected 400, got %d; body: %s", term, w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response not valid JSON: %v; body: %s", err, w.Body.String())
		}
		if resp["errcode"] != "M_INVALID_PARAM" {
			t.Errorf("search_term=%q: expected errcode M_INVALID_PARAM, got %v", term, resp["errcode"])
		}
		if db.capturedPattern != "" {
			t.Errorf("search_term=%q: DB must not be called for empty input", term)
		}
	}
}

// TestUserDirectory_RejectsWildcardOnlyInput (AC #1 + AC #2: len < 2 → 400)
//
// "%" is a single character and consists only of a LIKE metacharacter.
// After escaping, it would be "\%" — still meaningful in theory — but the
// validation must reject any search term shorter than 2 characters (after trim)
// with 400 M_INVALID_PARAM, before any escaping or DB call.
//
// Acceptance test: TestUserDirectory_SearchTerm_Percent_Returns400
func TestUserDirectory_RejectsWildcardOnlyInput(t *testing.T) {
	db := &mockUserDirectoryDB{}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	singleCharTerms := []string{"%", "_", "a", "z", "1"}
	for _, term := range singleCharTerms {
		body := map[string]any{"search_term": term}
		w := postUserDirectorySearch(t, handler, token, body)

		if w.Code != http.StatusBadRequest {
			t.Errorf("search_term=%q: expected 400, got %d; body: %s", term, w.Code, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response not valid JSON: %v", err)
		}
		if resp["errcode"] != "M_INVALID_PARAM" {
			t.Errorf("search_term=%q: expected M_INVALID_PARAM, got %v", term, resp["errcode"])
		}
	}
}

// TestUserDirectory_SearchTerm_Percent_Returns400 (Acceptance Test #1)
//
// Explicit acceptance test from the story: {"search_term":"%"} → 400.
// Single-char term, metachar-only: MUST be rejected before DB.
func TestUserDirectory_SearchTerm_Percent_Returns400(t *testing.T) {
	db := &mockUserDirectoryDB{}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	w := postUserDirectorySearch(t, handler, token, map[string]any{"search_term": "%"})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for search_term=%%, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_INVALID_PARAM" {
		t.Errorf("expected M_INVALID_PARAM, got %v", resp["errcode"])
	}
	// DB must not have been queried.
	if db.capturedPattern != "" {
		t.Errorf("DB must not be called for wildcard-only single-char input")
	}
}

// TestUserDirectory_RejectsTooLongSearchTerm (AC #1: len > 64 → 400)
//
// A search term of 65 runes must be rejected with 400 M_INVALID_PARAM.
func TestUserDirectory_RejectsTooLongSearchTerm(t *testing.T) {
	db := &mockUserDirectoryDB{}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	long := strings.Repeat("a", 65)
	w := postUserDirectorySearch(t, handler, token, map[string]any{"search_term": long})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for 65-char term, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_INVALID_PARAM" {
		t.Errorf("expected M_INVALID_PARAM, got %v", resp["errcode"])
	}
}

// TestUserDirectory_AcceptsValidSearchTerm (AC #1 happy path)
//
// A two-character search term must reach the DB and return 200.
func TestUserDirectory_AcceptsValidSearchTerm(t *testing.T) {
	db := &mockUserDirectoryDB{
		rows: []UserDirectoryResult{
			{UserID: "@al:test.local", DisplayName: "al"},
		},
	}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	w := postUserDirectorySearch(t, handler, token, map[string]any{"search_term": "al"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatalf("expected results array, got %T", resp["results"])
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

// TestUserDirectory_SearchTerm_Alice_MatchesLiteral (Acceptance Test #2)
//
// Given user "alice%test", searching for "alice%" must return 0 rows (because
// the % is escaped → literal match), but searching for "alice" must return the row.
//
// This test drives the DB mock directly: we set capturedPattern and assert the
// handler sends the correct escaped pattern. The DB is then free to simulate
// the zero-match and one-match scenarios without a real PostgreSQL.
func TestUserDirectory_SearchTerm_Alice_MatchesLiteral(t *testing.T) {
	// Scenario A: search_term = "alice%" → escaped pattern must be "%alice\%%"
	// The mock returns 0 rows (simulating no literal "alice%" user match).
	t.Run("alice_percent_returns_zero_rows", func(t *testing.T) {
		db := &mockUserDirectoryDB{rows: []UserDirectoryResult{}}
		handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
		token := makeToken()

		w := postUserDirectorySearch(t, handler, token, map[string]any{"search_term": "alice%"})

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
		}
		// Assert the handler passed the correctly escaped ILIKE pattern to the DB.
		// The backslash-percent literal must survive the interface boundary.
		expectedPattern := `%alice\%%`
		if db.capturedPattern != expectedPattern {
			t.Errorf("captured DB pattern = %q, want %q", db.capturedPattern, expectedPattern)
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		results := resp["results"].([]any)
		if len(results) != 0 {
			t.Errorf("expected 0 results for escaped %% search, got %d", len(results))
		}
	})

	// Scenario B: search_term = "alice" → escaped pattern must be "%alice%"
	// The mock returns 1 row (simulating a real "alice%test" user being found).
	t.Run("alice_literal_returns_one_row", func(t *testing.T) {
		db := &mockUserDirectoryDB{
			rows: []UserDirectoryResult{
				{UserID: "@alice%test:test.local", DisplayName: "alice%test"},
			},
		}
		handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
		token := makeToken()

		w := postUserDirectorySearch(t, handler, token, map[string]any{"search_term": "alice"})

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
		}
		expectedPattern := "%alice%"
		if db.capturedPattern != expectedPattern {
			t.Errorf("captured DB pattern = %q, want %q", db.capturedPattern, expectedPattern)
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		results := resp["results"].([]any)
		if len(results) != 1 {
			t.Errorf("expected 1 result for literal search, got %d", len(results))
		}
	})
}

// TestUserDirectory_NoPanic_OnMalformedUID (AC #4 + Acceptance Test #3)
//
// The handler builds DisplayName by slicing uid between '@' and ':'.
// If uid contains no ':', strings.IndexByte returns -1 and uid[1:-1] panics.
//
// Fix required: i := strings.IndexByte(uid, ':'); if i <= 0 { continue }
//
// This test exercises the panic path: mockUserDirectoryDB returns a row whose
// UserID has no ':' character. The handler must skip it and return 200 with an
// empty results array — NOT panic.
//
// White-box: tests the guard introduced by the fix via observable behaviour
// (no crash, empty results) rather than implementation details.
func TestUserDirectory_NoPanic_OnMalformedUID(t *testing.T) {
	db := &mockUserDirectoryDB{
		rows: []UserDirectoryResult{
			// malformed: no colon, would cause uid[1:strings.Index(uid,":")] panic
			{UserID: "noformat", DisplayName: ""},
		},
	}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	// If the handler panics, httptest will catch it and the test will fail with
	// a panic message — no explicit panic recovery needed in the test itself.
	w := postUserDirectorySearch(t, handler, token, map[string]any{"search_term": "no"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (malformed uid skipped), got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatalf("expected results key to be an array, got %T", resp["results"])
	}
	if len(results) != 0 {
		t.Errorf("expected malformed uid to be skipped (0 results), got %d", len(results))
	}
}

// TestUserDirectory_NoPanic_OnMissingColon (Acceptance Test #3 alias)
//
// Standalone test for the "uid without ':'" panic-guard.
// Uses its own mock and assertions — not a delegate.
func TestUserDirectory_NoPanic_OnMissingColon(t *testing.T) {
	db := &mockUserDirectoryDB{
		rows: []UserDirectoryResult{
			// uid with no colon — would panic without the guard
			{UserID: "nocolon", DisplayName: ""},
			// a valid uid to confirm it is still returned
			{UserID: "@alice:test.local", DisplayName: "Alice"},
		},
	}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	w := postUserDirectorySearch(t, handler, token, map[string]any{"search_term": "no"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (malformed uid skipped), got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatalf("expected results key to be an array, got %T", resp["results"])
	}
	// "nocolon" is skipped; "@alice:test.local" survives.
	if len(results) != 1 {
		t.Errorf("expected 1 result (valid uid), got %d", len(results))
	}
	first := results[0].(map[string]any)
	if first["user_id"] != "@alice:test.local" {
		t.Errorf("expected user_id @alice:test.local, got %v", first["user_id"])
	}
}

// ─── Result Cap Tests (AC #5) ─────────────────────────────────────────────────

// TestUserDirectory_LimitClampsAt100 (AC #5: req.Limit > 100 → clamp to 100)
//
// When the client sends {"limit": 200}, the handler must clamp to 100 and
// forward limit=100 to the DB layer.
func TestUserDirectory_LimitClampsAt100(t *testing.T) {
	db := &mockUserDirectoryDB{rows: []UserDirectoryResult{}}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	w := postUserDirectorySearch(t, handler, token, map[string]any{
		"search_term": "al",
		"limit":       200,
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if db.capturedLimit != 100 {
		t.Errorf("expected clamped limit=100 passed to DB, got %d", db.capturedLimit)
	}
}

// TestUserDirectory_LimitDefaultsTo10 (AC #5: req.Limit == 0 → default 10)
//
// When the client omits "limit" (JSON default 0), the handler must use 10.
func TestUserDirectory_LimitDefaultsTo10(t *testing.T) {
	db := &mockUserDirectoryDB{rows: []UserDirectoryResult{}}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	w := postUserDirectorySearch(t, handler, token, map[string]any{
		"search_term": "al",
		// "limit" intentionally omitted → JSON int zero-value
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if db.capturedLimit != 10 {
		t.Errorf("expected default limit=10 passed to DB, got %d", db.capturedLimit)
	}
}

// TestUserDirectory_NegativeLimitDefaultsTo10 (AC #5: negative limit → default 10)
//
// A negative limit is invalid — the handler must treat it the same as zero
// and fall back to the default of 10. Without this guard, a negative value
// would be forwarded to SQL LIMIT which is invalid in PostgreSQL.
func TestUserDirectory_NegativeLimitDefaultsTo10(t *testing.T) {
	db := &mockUserDirectoryDB{rows: []UserDirectoryResult{}}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	w := postUserDirectorySearch(t, handler, token, map[string]any{
		"search_term": "al",
		"limit":       -5,
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if db.capturedLimit != 10 {
		t.Errorf("expected default limit=10 for negative input, got %d", db.capturedLimit)
	}
}

// ─── Auth Tests ───────────────────────────────────────────────────────────────

// TestUserDirectory_Unauthenticated_Returns401
//
// The endpoint requires a valid JWT. A request without Authorization must be
// rejected with 401 M_MISSING_TOKEN before the handler is reached.
func TestUserDirectory_Unauthenticated_Returns401(t *testing.T) {
	db := &mockUserDirectoryDB{}
	handler, _ := buildAuthedUserDirectoryHandler(t, db)

	// No token.
	w := postUserDirectorySearch(t, handler, "", map[string]any{"search_term": "alice"})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
	// DB must not have been called.
	if db.capturedPattern != "" {
		t.Errorf("DB must not be called for unauthenticated request")
	}
}

// TestUserDirectory_ResponseShape
//
// Validates that the 200 response contains "results" (array) and "limited"
// (bool) keys, matching the Matrix spec for user_directory/search.
func TestUserDirectory_ResponseShape(t *testing.T) {
	db := &mockUserDirectoryDB{
		rows: []UserDirectoryResult{
			{UserID: "@alice:test.local", DisplayName: "alice"},
		},
	}
	handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
	token := makeToken()

	w := postUserDirectorySearch(t, handler, token, map[string]any{"search_term": "al"})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not valid JSON: %v", err)
	}
	if _, ok := resp["results"]; !ok {
		t.Error("response missing 'results' key")
	}
	if _, ok := resp["limited"]; !ok {
		t.Error("response missing 'limited' key")
	}
	results := resp["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	first := results[0].(map[string]any)
	if first["user_id"] != "@alice:test.local" {
		t.Errorf("expected user_id @alice:test.local, got %v", first["user_id"])
	}
	if first["display_name"] != "alice" {
		t.Errorf("expected display_name alice, got %v", first["display_name"])
	}
}

// ─── MAJOR #1: Escape Pattern Forwarding Tests (AC #3) ───────────────────────

// TestUserDirectory_EscapePatternForwardedToDBWithPercent (MAJOR #1)
//
// Proves that the handler correctly escapes LIKE metacharacters and forwards
// the escaped pattern — including the surrounding %…% wildcard — to the DB
// layer. The mockUserDirectoryDB.capturedPattern is the observable proof that
// the ESCAPE clause is meaningful: a wrong pattern here would cause
// user-enumeration via full-table scans on a real PostgreSQL.
//
// Test cases:
//   - "test%user"  → DB receives "%test\%user%"
//   - "a_b"        → DB receives "%a\_b%"
//   - "a\\b"       → DB receives "%a\\\\b%"  (backslash escaped to \\)
func TestUserDirectory_EscapePatternForwardedToDBWithPercent(t *testing.T) {
	cases := []struct {
		searchTerm      string
		expectedPattern string
	}{
		{
			searchTerm:      "test%user",
			expectedPattern: `%test\%user%`,
		},
		{
			searchTerm:      "a_b",
			expectedPattern: `%a\_b%`,
		},
		{
			searchTerm:      `a\b`,
			expectedPattern: `%a\\b%`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.searchTerm, func(t *testing.T) {
			db := &mockUserDirectoryDB{rows: []UserDirectoryResult{}}
			handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
			token := makeToken()

			w := postUserDirectorySearch(t, handler, token, map[string]any{
				"search_term": tc.searchTerm,
			})

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
			}
			// The captured pattern is the exact string forwarded to the DB SearchUsers call.
			// This proves the ESCAPE clause in the SQL query is exercised with correctly
			// escaped metacharacters — preventing LIKE pattern injection.
			if db.capturedPattern != tc.expectedPattern {
				t.Errorf("search_term=%q: DB received pattern %q, want %q",
					tc.searchTerm, db.capturedPattern, tc.expectedPattern)
			}
		})
	}
}

// ─── MINOR #1: Boundary Tests (AC #1) ────────────────────────────────────────

// TestUserDirectory_BoundarySearchTermLength (MINOR #1)
//
// Explicit boundary-value tests for the search_term length validation:
//   - len == 1 → 400  (already tested implicitly; here made explicit)
//   - len == 2 → 200  (lower valid boundary, explicit)
//   - len == 64 → 200 (upper valid boundary, explicit)
//   - len == 65 → 400 (one over the limit, explicit)
func TestUserDirectory_BoundarySearchTermLength(t *testing.T) {
	cases := []struct {
		name         string
		term         string
		expectedCode int
	}{
		{
			name:         "len_1_rejected",
			term:         "a",
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "len_2_accepted",
			term:         "al",
			expectedCode: http.StatusOK,
		},
		{
			name:         "len_64_accepted",
			term:         strings.Repeat("a", 64),
			expectedCode: http.StatusOK,
		},
		{
			name:         "len_65_rejected",
			term:         strings.Repeat("a", 65),
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			db := &mockUserDirectoryDB{rows: []UserDirectoryResult{}}
			handler, makeToken := buildAuthedUserDirectoryHandler(t, db)
			token := makeToken()

			w := postUserDirectorySearch(t, handler, token, map[string]any{
				"search_term": tc.term,
			})

			if w.Code != tc.expectedCode {
				t.Errorf("search_term len=%d: expected %d, got %d; body: %s",
					len([]rune(tc.term)), tc.expectedCode, w.Code, w.Body.String())
			}
			if tc.expectedCode == http.StatusBadRequest {
				var resp map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("response not valid JSON: %v", err)
				}
				if resp["errcode"] != "M_INVALID_PARAM" {
					t.Errorf("expected errcode M_INVALID_PARAM, got %v", resp["errcode"])
				}
			}
		})
	}
}
