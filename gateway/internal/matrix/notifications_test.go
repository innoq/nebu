package matrix

// ─── Story 7-29: Notifications API — GET /_matrix/client/v3/notifications ──
//
// Tests written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file MUST FAIL until notifications.go is created and
// the route is registered in main.go.
//
// Acceptance Criteria covered:
//   AC1 — GET /notifications → 200 with {"next_token":"...","notifications":[...]}
//   AC2 — from cursor returns the next page of results
//   AC3 — only=highlight filters to notifications whose actions contain "highlight"
//   AC4 — limit defaults to 50, max 200; values > 200 → 400 M_INVALID_PARAM
//   AC5 — empty result: notifications is [], next_token absent or empty
//   AC7 — JWT required; no token → 401 M_MISSING_TOKEN
//
// Design:
//   - NotificationsHandler takes a NotificationsDB interface (consumer-defined, ADR-009).
//   - mockNotificationsDB implements NotificationsDB in-memory for deterministic tests.
//   - buildAuthedNotificationsHandler wires JWTMiddleware → NotificationsHandler.
//   - Cursor encode/decode helpers (encodeCursor / decodeCursor) are tested directly.
//
// NOTE: NotificationsHandler, NotificationsConfig, NewNotificationsHandler,
// NotificationsDB, NotificationRow, NotificationItem, encodeCursor, decodeCursor
// are declared in gateway/internal/matrix/notifications.go — which does NOT exist
// yet. Every test in this file MUST fail with a compilation error until
// notifications.go is created.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Mock NotificationsDB ─────────────────────────────────────────────────────

// mockNotificationsDB implements NotificationsDB in memory.
// Rows are stored in insertion order; GetNotifications returns them newest-first (reverse).
type mockNotificationsDB struct {
	mu   sync.Mutex
	rows []NotificationRow // stored in ascending id order
	err  error              // inject an error for all GetNotifications calls
}

func newMockNotificationsDB() *mockNotificationsDB {
	return &mockNotificationsDB{}
}

// addRow appends a notification row (caller controls the id for determinism).
func (m *mockNotificationsDB) addRow(row NotificationRow) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, row)
}

// GetNotifications implements NotificationsDB.
//
// Filters by fromID (id < fromID when > 0), onlyHighlight, returns limit rows newest-first.
// nextID is the id of the last returned row (0 when no more rows exist beyond the page).
func (m *mockNotificationsDB) GetNotifications(
	ctx context.Context,
	userID string,
	fromID int64,
	limit int,
	onlyHighlight bool,
) ([]NotificationRow, int64, error) {
	if m.err != nil {
		return nil, 0, m.err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Collect matching rows newest-first (reverse iteration over ascending list).
	var matching []NotificationRow
	for i := len(m.rows) - 1; i >= 0; i-- {
		row := m.rows[i]
		if row.RoomID == "" {
			// skip sentinel rows if any
		}
		// cursor filter: if fromID > 0 only include rows with id < fromID.
		if fromID > 0 && row.ID >= fromID {
			continue
		}
		// highlight filter.
		if onlyHighlight {
			var actions []string
			if err := json.Unmarshal(row.ActionsRaw, &actions); err != nil {
				continue
			}
			hasHighlight := false
			for _, a := range actions {
				if a == "highlight" {
					hasHighlight = true
					break
				}
			}
			if !hasHighlight {
				continue
			}
		}
		matching = append(matching, row)
	}

	// Apply limit.
	if limit > 0 && len(matching) > limit {
		matching = matching[:limit]
	}

	// Calculate nextID: if we trimmed the list, there are more pages.
	// nextID is the id of the last item returned (smallest id in the page) so the
	// caller can use it as the next cursor.
	var nextID int64
	if len(matching) == limit && limit > 0 {
		// Check if there are more rows beyond this page.
		lastID := matching[len(matching)-1].ID
		for i := len(m.rows) - 1; i >= 0; i-- {
			row := m.rows[i]
			if fromID > 0 && row.ID >= fromID {
				continue
			}
			if onlyHighlight {
				var actions []string
				_ = json.Unmarshal(row.ActionsRaw, &actions)
				hasHighlight := false
				for _, a := range actions {
					if a == "highlight" {
						hasHighlight = true
						break
					}
				}
				if !hasHighlight {
					continue
				}
			}
			if row.ID < lastID {
				// There's at least one more row after our page.
				nextID = lastID
				break
			}
		}
	}

	return matching, nextID, nil
}

// ─── Helper ───────────────────────────────────────────────────────────────────

// buildAuthedNotificationsHandler wires JWTMiddleware → NotificationsHandler on a mux.
// JWT sub is "test-sub-123" → authenticated user_id "@test-sub-123:test.local".
func buildAuthedNotificationsHandler(t *testing.T, db *mockNotificationsDB) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)
	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil, "test.local")

	handler := NewNotificationsHandler(NotificationsConfig{DB: db})

	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/client/v3/notifications",
		jwtMiddleware(http.HandlerFunc(handler.GetNotifications)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// makeNotifyRow creates a "notify"-only notification row with the given id.
func makeNotifyRow(id int64) NotificationRow {
	return NotificationRow{
		ID:         id,
		RoomID:     fmt.Sprintf("!room%d:test.local", id),
		ActionsRaw: json.RawMessage(`["notify"]`),
		EventRaw:   json.RawMessage(`{"type":"m.room.message","room_id":"!room1:test.local","event_id":"$ev1","sender":"@alice:test.local","content":{"msgtype":"m.text","body":"hi"}}`),
		Read:       false,
		TS:         time.Now().UnixMilli(),
	}
}

// makeHighlightRow creates a "notify"+"highlight" notification row with the given id.
func makeHighlightRow(id int64) NotificationRow {
	row := makeNotifyRow(id)
	row.ActionsRaw = json.RawMessage(`["notify","highlight"]`)
	return row
}

// ─── AC5: Empty result ────────────────────────────────────────────────────────

func TestGetNotifications_NoRows_ReturnsEmpty(t *testing.T) {
	db := newMockNotificationsDB()
	mux, makeToken := buildAuthedNotificationsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		NextToken     string             `json:"next_token"`
		Notifications []NotificationItem `json:"notifications"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if resp.Notifications == nil || len(resp.Notifications) != 0 {
		t.Errorf("expected empty notifications array, got %v", resp.Notifications)
	}
	if resp.NextToken != "" {
		t.Errorf("expected empty next_token, got %q", resp.NextToken)
	}
}

// ─── AC1: Returns paged list ──────────────────────────────────────────────────

func TestGetNotifications_ThreeRows_LimitTwo_ReturnsTwoWithNextToken(t *testing.T) {
	db := newMockNotificationsDB()
	// Add 3 rows in ascending id order.
	db.addRow(makeNotifyRow(1))
	db.addRow(makeNotifyRow(2))
	db.addRow(makeNotifyRow(3))

	mux, makeToken := buildAuthedNotificationsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications?limit=2", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		NextToken     string             `json:"next_token"`
		Notifications []NotificationItem `json:"notifications"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if len(resp.Notifications) != 2 {
		t.Errorf("expected 2 notifications, got %d", len(resp.Notifications))
	}
	if resp.NextToken == "" {
		t.Error("expected non-empty next_token, got empty")
	}

	// Verify each item has the required fields.
	for i, item := range resp.Notifications {
		if len(item.Actions) == 0 {
			t.Errorf("item[%d]: expected non-empty actions", i)
		}
		if item.Event == nil {
			t.Errorf("item[%d]: expected non-nil event", i)
		}
		if item.RoomID == "" {
			t.Errorf("item[%d]: expected non-empty room_id", i)
		}
		if item.TS == 0 {
			t.Errorf("item[%d]: expected non-zero ts", i)
		}
	}
}

// ─── AC2: From cursor returns second page ─────────────────────────────────────

func TestGetNotifications_FromCursor_ReturnsRemainingItem(t *testing.T) {
	db := newMockNotificationsDB()
	db.addRow(makeNotifyRow(1))
	db.addRow(makeNotifyRow(2))
	db.addRow(makeNotifyRow(3))

	mux, makeToken := buildAuthedNotificationsHandler(t, db)
	token := makeToken()

	// First page: limit=2, get next_token.
	req1 := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications?limit=2", nil)
	req1.Header.Set("Authorization", "Bearer "+token)

	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first page: expected 200, got %d; body: %s", w1.Code, w1.Body.String())
	}

	var resp1 struct {
		NextToken     string             `json:"next_token"`
		Notifications []NotificationItem `json:"notifications"`
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("first page: parse error: %v; body: %s", err, w1.Body.String())
	}
	if resp1.NextToken == "" {
		t.Fatal("first page: expected next_token, got empty")
	}

	// Second page using the cursor.
	url2 := fmt.Sprintf("/_matrix/client/v3/notifications?limit=2&from=%s", resp1.NextToken)
	req2 := httptest.NewRequest(http.MethodGet, url2, nil)
	req2.Header.Set("Authorization", "Bearer "+token)

	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second page: expected 200, got %d; body: %s", w2.Code, w2.Body.String())
	}

	var resp2 struct {
		NextToken     string             `json:"next_token"`
		Notifications []NotificationItem `json:"notifications"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("second page: parse error: %v; body: %s", err, w2.Body.String())
	}
	if len(resp2.Notifications) != 1 {
		t.Errorf("second page: expected 1 notification, got %d", len(resp2.Notifications))
	}
	if resp2.NextToken != "" {
		t.Errorf("second page: expected empty next_token, got %q", resp2.NextToken)
	}
}

// ─── AC3: only=highlight filter ───────────────────────────────────────────────

func TestGetNotifications_OnlyHighlight_FiltersCorrectly(t *testing.T) {
	db := newMockNotificationsDB()
	db.addRow(makeNotifyRow(1))     // actions: ["notify"]
	db.addRow(makeHighlightRow(2))  // actions: ["notify","highlight"]

	mux, makeToken := buildAuthedNotificationsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications?only=highlight", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Notifications []NotificationItem `json:"notifications"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if len(resp.Notifications) != 1 {
		t.Errorf("expected 1 highlight notification, got %d", len(resp.Notifications))
	}
}

// ─── AC4: limit > 200 → 400 M_INVALID_PARAM ──────────────────────────────────

func TestGetNotifications_LimitTooLarge_Returns400(t *testing.T) {
	db := newMockNotificationsDB()
	mux, makeToken := buildAuthedNotificationsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications?limit=999", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %q", errResp["errcode"])
	}
	if !strings.Contains(errResp["error"], "200") {
		t.Errorf("expected error message to mention 200 (max limit), got %q", errResp["error"])
	}
}

// ─── AC4: limit=200 is accepted ───────────────────────────────────────────────

func TestGetNotifications_LimitAtMax_Accepted(t *testing.T) {
	db := newMockNotificationsDB()
	mux, makeToken := buildAuthedNotificationsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications?limit=200", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (limit=200 is valid), got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── AC4: limit=0 is rejected ─────────────────────────────────────────────────

func TestGetNotifications_LimitZero_Returns400(t *testing.T) {
	db := newMockNotificationsDB()
	mux, makeToken := buildAuthedNotificationsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications?limit=0", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── AC7: JWT required — no token → 401 ──────────────────────────────────────

func TestGetNotifications_Unauthenticated_Returns401(t *testing.T) {
	db := newMockNotificationsDB()
	mux, _ := buildAuthedNotificationsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications", nil)
	// No Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "M_MISSING_TOKEN") {
		t.Errorf("expected M_MISSING_TOKEN in body, got: %s", body)
	}
}

// ─── Invalid from cursor → 400 M_INVALID_PARAM ────────────────────────────────

func TestGetNotifications_InvalidCursor_Returns400(t *testing.T) {
	db := newMockNotificationsDB()
	mux, makeToken := buildAuthedNotificationsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications?from=not-a-valid-cursor!!!", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %q", errResp["errcode"])
	}
}

// ─── DB error → 500 M_UNKNOWN ────────────────────────────────────────────────

func TestGetNotifications_DBError_Returns500(t *testing.T) {
	db := newMockNotificationsDB()
	db.err = fmt.Errorf("simulated DB failure")

	mux, makeToken := buildAuthedNotificationsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_UNKNOWN" {
		t.Errorf("expected errcode M_UNKNOWN, got %q", errResp["errcode"])
	}
}

// ─── Cursor encoding round-trip ───────────────────────────────────────────────

func TestEncodeDecode_Cursor_RoundTrip(t *testing.T) {
	tests := []int64{1, 42, 999999, 1<<32 - 1}
	for _, id := range tests {
		encoded := encodeCursor(id)
		if encoded == "" {
			t.Errorf("id=%d: encodeCursor returned empty string", id)
		}
		decoded, err := decodeCursor(encoded)
		if err != nil {
			t.Errorf("id=%d: decodeCursor(%q) error: %v", id, encoded, err)
		}
		if decoded != id {
			t.Errorf("id=%d: round-trip failed, got %d", id, decoded)
		}
	}
}

func TestDecodeCursor_InvalidBase64_Error(t *testing.T) {
	_, err := decodeCursor("!!!not-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
}

func TestDecodeCursor_ValidBase64_NotInteger_Error(t *testing.T) {
	// Base64url of "hello" — valid base64 but not a number.
	_, err := decodeCursor("aGVsbG8")
	if err == nil {
		t.Fatal("expected error when base64 decodes to non-integer, got nil")
	}
}

func TestDecodeCursor_Zero_Error(t *testing.T) {
	// "0" is not a valid cursor (id must be > 0).
	// encodeCursor(0) produces "MA" (base64url of "0") — decodeCursor must reject id=0.
	// Decode "MA" directly to test the rejection of zero without calling encodeCursor.
	_, err := decodeCursor("MA") // base64url("0")
	if err == nil {
		t.Fatal("expected error for cursor encoding 0, got nil")
	}
}

// ─── Response shape: notifications array never null ───────────────────────────

func TestGetNotifications_NeverNullNotifications(t *testing.T) {
	db := newMockNotificationsDB()
	mux, makeToken := buildAuthedNotificationsHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, `"notifications":null`) {
		t.Errorf("'notifications' must not be null; body: %s", body)
	}
}
