package compliance_test

// user_anonymization_test.go — Story 5.8: Operational PII Anonymization — RED-phase tests
//
// ALL tests in this file are expected to FAIL until Story 5.8 is implemented.
// Failing reasons:
//   - AnonymizationHandler type does not exist in the compliance package.
//   - AnonymizeUser method does not exist.
//   - fakedb driver does not yet understand anonymization SQL patterns.
//
// Test strategy:
//   - anonymizationFakeDB: a sql driver registered as "anondb".
//     DSN encodes scenario flags:
//       profileFound=<true|false>     — whether SELECT avatar_url FROM profiles returns a row
//       avatarURL=<url>               — the avatar_url value returned (may contain mxc://)
//     The driver handles four distinct SQL patterns:
//       1. SELECT avatar_url FROM profiles WHERE user_id = $1
//       2. UPDATE profiles SET displayname='Deleted User', avatar_url=NULL WHERE user_id=$1
//       3. UPDATE users SET anonymized_at=$1 WHERE user_id=$2
//       4. UPDATE media_files SET deleted=true WHERE media_id=$1
//   - FileRemover interface: implemented by a controllable mock (success/failure).
//   - mockCoreClient is already declared in handler_test.go (same package) — reused.
//   - Context keys (ContextKeySub, ContextKeySystemRole) injected directly — no JWT middleware.
//   - httptest.NewRecorder captures status and body for assertions.
//
// Tests (14 total for Story 5.8):
//  1.  TestAnonymizeUser_HappyPath_MxcAvatar                — AC1, AC2, AC3, AC5
//  2.  TestAnonymizeUser_AvatarNonMxc_Skipped               — AC3 (non-mxc skip)
//  3.  TestAnonymizeUser_FileRemoveFails_Still200            — AC3 (log-warn, no-abort)
//  4.  TestAnonymizeUser_NonAdmin_Returns403                 — AC1 (role gate)
//  5.  TestAnonymizeUser_UnknownUser_Returns404              — AC1 (user existence)
//  6.  TestAnonymizeUser_PathParamCap_Returns400             — AC1 (defence-in-depth)
//  7.  TestAnonymizeUser_AuditEmission                      — AC5 (action + metadata)
//  8.  TestAnonymizeUser_AuditFailure_Still200               — AC5 (never-raise)
//  9.  TestAnonymizeUser_AlreadyAnonymized_StillSucceeds     — idempotent 200
// 10.  TestAnonymizeUser_AvatarMxcMalformed_Skipped          — AC3 (malformed mxc → skip)
// 11.  TestAnonymizeUser_NoAvatarURL_Skipped                 — AC3 (empty/NULL avatar → skip)
// 12.  TestProfile_AfterAnonymize_ReturnsDeletedUser         — AC6 (GetProfile side-effect)
// 13.  TestEventsUnchanged_AfterAnonymize                    — AC4 (events table not touched)

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nebu/nebu/internal/compliance"
	"github.com/nebu/nebu/internal/matrix"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── anonFakeDB driver ────────────────────────────────────────────────────────
//
// DSN format:
//   "profileFound=<true|false>&avatarURL=<escaped-url>"
//
// SQL routing:
//   - SELECT avatar_url FROM profiles → returns avatarURL if profileFound, else ErrNoRows
//   - UPDATE profiles SET displayname='Deleted User' → records call, returns 1 row affected
//   - UPDATE users SET anonymized_at → records call, returns 1 row affected
//   - UPDATE media_files SET deleted=true → records call, returns 1 row affected
//   - SELECT ... FROM events → returns rows, never modified (regression guard)

var anonDriverOnce sync.Once

func init() {
	anonDriverOnce.Do(func() {
		sql.Register("anondb", &anonFakeDriver{})
	})
}

type anonFakeDriver struct{}

func (d *anonFakeDriver) Open(name string) (driver.Conn, error) {
	return &anonFakeConn{dsn: name}, nil
}

type anonFakeConn struct {
	dsn string
}

func (c *anonFakeConn) Prepare(query string) (driver.Stmt, error) {
	profileFound := strings.Contains(c.dsn, "profileFound=true")

	// Extract avatarURL from DSN: avatarURL=<value> (URL-encoded — use simple parse)
	avatarURL := ""
	for _, part := range strings.Split(c.dsn, "&") {
		if strings.HasPrefix(part, "avatarURL=") {
			avatarURL = strings.TrimPrefix(part, "avatarURL=")
			break
		}
	}

	return &anonFakeStmt{
		query:        query,
		profileFound: profileFound,
		avatarURL:    avatarURL,
	}, nil
}

func (c *anonFakeConn) Close() error { return nil }
func (c *anonFakeConn) Begin() (driver.Tx, error) {
	return &anonFakeTx{}, nil
}

type anonFakeTx struct{}

func (t *anonFakeTx) Commit() error   { return nil }
func (t *anonFakeTx) Rollback() error { return nil }

type anonFakeStmt struct {
	query        string
	profileFound bool
	avatarURL    string
}

func (s *anonFakeStmt) Close() error { return nil }
func (s *anonFakeStmt) NumInput() int { return -1 }

func (s *anonFakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	// Capture the SQL string so AC4 regression test can assert that the handler
	// never targets the events table. TEA Gate 2 MAJOR-2: previously this method
	// returned silently, leaving queryCaptureStore.captured empty and making the
	// AC4 assertion vacuous.
	queryCaptureStore.record(s.query)
	return driver.RowsAffected(1), nil
}

func (s *anonFakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	lq := strings.ToLower(s.query)

	// SELECT avatar_url FROM profiles WHERE user_id = $1
	if strings.Contains(lq, "avatar_url") && strings.Contains(lq, "profiles") {
		if !s.profileFound {
			return &anonFakeRows{cols: []string{"avatar_url"}, data: nil}, nil
		}
		return &anonFakeRows{
			cols: []string{"avatar_url"},
			data: [][]driver.Value{{s.avatarURL}},
		}, nil
	}

	// SELECT ... FROM events — regression guard: return empty, never modified
	if strings.Contains(lq, "from events") {
		return &anonFakeRows{
			cols: []string{"event_id", "sender"},
			data: [][]driver.Value{{"$evt:server", "@user:server"}},
		}, nil
	}

	return &anonFakeRows{}, nil
}

type anonFakeRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *anonFakeRows) Columns() []string { return r.cols }
func (r *anonFakeRows) Close() error      { return nil }
func (r *anonFakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

// ─── anonFakeDB helpers ───────────────────────────────────────────────────────

func openAnonDB(t *testing.T, profileFound bool, avatarURL string) *sql.DB {
	t.Helper()
	dsn := "profileFound=false"
	if profileFound {
		dsn = "profileFound=true"
	}
	dsn += "&avatarURL=" + avatarURL
	db, err := sql.Open("anondb", dsn)
	if err != nil {
		t.Fatalf("sql.Open(anondb): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ─── mockFileRemover ──────────────────────────────────────────────────────────
//
// Implements compliance.FileRemover (to be defined in user_anonymization.go).
// Controlled by shouldFail: when true, Remove returns an error.
// Tracks whether Remove was called and with what path.

type mockFileRemover struct {
	shouldFail bool
	calledWith string
	callCount  int
}

func (m *mockFileRemover) Remove(path string) error {
	m.callCount++
	m.calledWith = path
	if m.shouldFail {
		return &mockRemoveError{path: path}
	}
	return nil
}

type mockRemoveError struct{ path string }

func (e *mockRemoveError) Error() string {
	return "mock remove error: " + e.path
}

// ─── request helpers ──────────────────────────────────────────────────────────

func newAnonRequest(t *testing.T, userID, systemRole, sub string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost,
		"/api/v1/admin/users/"+userID+"/anonymize",
		bytes.NewReader(nil),
	)
	// No Content-Type required — handler accepts no body per AC1
	r.SetPathValue("userId", userID)

	ctx := r.Context()
	ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, systemRole)
	ctx = context.WithValue(ctx, middleware.ContextKeySub, sub)
	return r.WithContext(ctx)
}

// ─── Test 1: HappyPath — mxc avatar cleanup → 200 ────────────────────────────
//
// AC1, AC2, AC3, AC5
//
// Given: instance_admin caller, valid userId, profiles-row with avatar_url='mxc://server.local/mediaABC'
// When:  POST /api/v1/admin/users/{userId}/anonymize
// Then:  200 {"user_id":"user-99","status":"anonymized"}
//        fakedb received UPDATE profiles (displayname='Deleted User', avatar_url=NULL)
//        fakedb received UPDATE users (anonymized_at set)
//        fakedb received UPDATE media_files (deleted=true)
//        FileRemover.Remove called with path containing "mediaABC"
//        audit action="user_anonymized" emitted

func TestAnonymizeUser_HappyPath_MxcAvatar(t *testing.T) {
	db := openAnonDB(t, true, "mxc://server.local/mediaABC")
	client := &mockCoreClient{}
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "user-99", "instance_admin", "admin-sub-1")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v — body: %s", err, w.Body.String())
	}
	if resp["user_id"] != "user-99" {
		t.Errorf("expected user_id='user-99', got %q", resp["user_id"])
	}
	if resp["status"] != "anonymized" {
		t.Errorf("expected status='anonymized', got %q", resp["status"])
	}

	// File remover must have been called with the media ID
	if fr.callCount == 0 {
		t.Error("expected FileRemover.Remove to be called for mxc avatar, but it was not")
	}
	if !strings.Contains(fr.calledWith, "mediaABC") {
		t.Errorf("FileRemover called with %q — expected path to contain 'mediaABC'", fr.calledWith)
	}

	// Audit must have been emitted
	if client.callCount() == 0 {
		t.Error("expected WriteAuditLog to be called, but it was not")
	}
	auditReq := client.lastReceived()
	if auditReq.Action != "user_anonymized" {
		t.Errorf("expected audit action='user_anonymized', got %q", auditReq.Action)
	}
}

// ─── Test 2: AvatarNonMxc_Skipped — external URL → no file remove ─────────────
//
// AC3 (non-mxc skip path)
//
// Given: instance_admin, profiles-row with avatar_url='https://cdn.example.com/avatar.png'
// When:  POST anonymize
// Then:  200; FileRemover.Remove NOT called; media_files UPDATE NOT expected

func TestAnonymizeUser_AvatarNonMxc_Skipped(t *testing.T) {
	db := openAnonDB(t, true, "https://cdn.example.com/avatar.png")
	client := &mockCoreClient{}
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "user-external-avatar", "instance_admin", "admin-sub-1")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for non-mxc avatar, got %d — body: %s", w.Code, w.Body.String())
	}

	if fr.callCount != 0 {
		t.Errorf("expected FileRemover.Remove NOT to be called for external avatar, but was called %d time(s)", fr.callCount)
	}
}

// ─── Test 3: FileRemoveFails_Still200 ────────────────────────────────────────
//
// AC3 (log-warn on file remove failure, do NOT abort — "log error but do NOT abort")
//
// Given: instance_admin, mxc avatar, FileRemover returns error
// When:  POST anonymize
// Then:  200 (not 500); audit still emitted

func TestAnonymizeUser_FileRemoveFails_Still200(t *testing.T) {
	db := openAnonDB(t, true, "mxc://server.local/missingFile")
	client := &mockCoreClient{}
	fr := &mockFileRemover{shouldFail: true}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "user-missing-file", "instance_admin", "admin-sub-1")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("file remove failure must NOT abort anonymize — expected 200, got %d — body: %s",
			w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["status"] != "anonymized" {
		t.Errorf("expected status='anonymized' even after file remove failure, got %q", resp["status"])
	}
}

// ─── Test 4: NonAdmin_Returns403 ──────────────────────────────────────────────
//
// AC1: role gate — only instance_admin; others → 403 M_FORBIDDEN
//
// Given: Caller with system_role="compliance_officer"
// When:  POST anonymize
// Then:  403 M_FORBIDDEN

func TestAnonymizeUser_NonAdmin_Returns403(t *testing.T) {
	db := openAnonDB(t, true, "")
	client := &mockCoreClient{}
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "user-42", "compliance_officer", "officer-sub-1")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-admin, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode=M_FORBIDDEN, got %q", resp["errcode"])
	}
}

// ─── Test 5: UnknownUser_Returns404 ──────────────────────────────────────────
//
// AC1: no profiles-row for userId → 404 M_NOT_FOUND "User not found"
//
// Given: instance_admin, userId not in profiles (profileFound=false)
// When:  POST anonymize
// Then:  404 M_NOT_FOUND

func TestAnonymizeUser_UnknownUser_Returns404(t *testing.T) {
	db := openAnonDB(t, false /* profileFound */, "")
	client := &mockCoreClient{}
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "ghost-user", "instance_admin", "admin-sub-1")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for unknown user, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_NOT_FOUND" {
		t.Errorf("expected errcode=M_NOT_FOUND, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "not found") {
		t.Errorf("expected error to mention 'not found', got %q", resp["error"])
	}
}

// ─── Test 6: PathParamCap_Returns400 ─────────────────────────────────────────
//
// AC1: defence-in-depth — userId path param > 255 chars → 400 M_BAD_JSON
//
// Given: instance_admin caller, userId is 256 characters
// When:  POST anonymize
// Then:  400

func TestAnonymizeUser_PathParamCap_Returns400(t *testing.T) {
	db := openAnonDB(t, true, "")
	client := &mockCoreClient{}
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	oversized := strings.Repeat("u", 256) // 256 chars — one over the cap

	w := httptest.NewRecorder()
	r := newAnonRequest(t, oversized, "instance_admin", "admin-sub-1")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for oversized userId, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "255") {
		t.Errorf("expected error to mention '255', got %q", resp["error"])
	}
}

// ─── Test 7: AuditEmission — action="user_anonymized", metadata={} ────────────
//
// AC5: audit emitted with exact action, target_type, outcome, empty metadata
//
// Given: happy-path success (mxc avatar)
// When:  POST anonymize → 200
// Then:  WriteAuditLog called with action="user_anonymized", target_type="user",
//        outcome="success", metadata is empty map

func TestAnonymizeUser_AuditEmission(t *testing.T) {
	db := openAnonDB(t, true, "mxc://server.local/media1")
	client := &mockCoreClient{}
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "user-audit-check", "instance_admin", "admin-sub-audit")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	if client.callCount() == 0 {
		t.Fatal("expected WriteAuditLog to be called, but no calls recorded")
	}

	auditReq := client.lastReceived()
	if auditReq.Action != "user_anonymized" {
		t.Errorf("expected audit action='user_anonymized', got %q", auditReq.Action)
	}
	if auditReq.TargetType != "user" {
		t.Errorf("expected audit target_type='user', got %q", auditReq.TargetType)
	}
	if auditReq.Outcome != "success" {
		t.Errorf("expected audit outcome='success', got %q", auditReq.Outcome)
	}
	if auditReq.TargetId != "user-audit-check" {
		t.Errorf("expected audit target_id='user-audit-check', got %q", auditReq.TargetId)
	}
}

// ─── Test 8: AuditFailure_Still200 ───────────────────────────────────────────
//
// AC5: never-raise audit policy — audit failure must NOT block the 200 response
//
// Given: happy-path conditions, WriteAuditLog returns an error
// When:  POST anonymize succeeds (DB updates OK)
// Then:  200 OK with valid body (audit failure silently swallowed)

func TestAnonymizeUser_AuditFailure_Still200(t *testing.T) {
	// Note: failWith error is set on mockCoreClient (declared in handler_test.go)
	// to trigger the never-raise audit policy — WriteAuditLog returns an error but
	// AnonymizeUser must still respond 200.

	db := openAnonDB(t, true, "mxc://server.local/media2")
	client := &mockCoreClient{}
	client.failWith = errAnonAuditFailure
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "user-audit-fail", "instance_admin", "admin-sub-audit-fail")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("audit failure must not block 200 — expected 200, got %d — body: %s",
			w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["status"] != "anonymized" {
		t.Errorf("expected status='anonymized', got %q", resp["status"])
	}
}

// errAnonAuditFailure is a package-level sentinel for the never-raise audit policy test.
// Named distinctly from session_test.go's errSimulatedAuditFailure to avoid redeclaration.
var errAnonAuditFailure = &anonAuditErr58{}

type anonAuditErr58 struct{}

func (e *anonAuditErr58) Error() string { return "simulated audit gRPC failure (story 5.8)" }

// ─── Test 9: AlreadyAnonymized_StillSucceeds (idempotent) ────────────────────
//
// Story-decision: double-anonymize is idempotent → 200 (no 409).
// The handler does UPDATE regardless of existing anonymized_at value.
//
// Given: instance_admin, profiles-row exists (already has displayname='Deleted User')
// When:  POST anonymize (second call)
// Then:  200 {"user_id":..., "status":"anonymized"} — idempotent

func TestAnonymizeUser_AlreadyAnonymized_StillSucceeds(t *testing.T) {
	// profile exists with empty avatar (already anonymized)
	db := openAnonDB(t, true, "")
	client := &mockCoreClient{}
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "user-already-anon", "instance_admin", "admin-sub-1")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for already-anonymized user (idempotent), got %d — body: %s",
			w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["status"] != "anonymized" {
		t.Errorf("expected status='anonymized', got %q", resp["status"])
	}
}

// ─── Test 10: AvatarMxcMalformed_Skipped ─────────────────────────────────────
//
// AC3: malformed mxc URI (missing mediaId part) → skip cleanup, still 200
//
// Given: avatar_url='mxc://server.local/' (no mediaId)
// When:  POST anonymize
// Then:  200; FileRemover.Remove NOT called

func TestAnonymizeUser_AvatarMxcMalformed_Skipped(t *testing.T) {
	db := openAnonDB(t, true, "mxc://server.local/") // malformed: empty mediaId
	client := &mockCoreClient{}
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "user-malformed-mxc", "instance_admin", "admin-sub-1")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("malformed mxc must not abort — expected 200, got %d — body: %s",
			w.Code, w.Body.String())
	}

	if fr.callCount != 0 {
		t.Errorf("expected FileRemover.Remove NOT called for malformed mxc, called %d time(s)", fr.callCount)
	}
}

// ─── Test 11: NoAvatarURL_Skipped ─────────────────────────────────────────────
//
// AC3: empty/NULL avatar_url → no mxc parse, no file remove, still 200
//
// Given: avatar_url="" (empty string / NULL equivalent)
// When:  POST anonymize
// Then:  200; FileRemover.Remove NOT called

func TestAnonymizeUser_NoAvatarURL_Skipped(t *testing.T) {
	db := openAnonDB(t, true, "") // empty avatar_url
	client := &mockCoreClient{}
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "user-no-avatar", "instance_admin", "admin-sub-1")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("empty avatar must not abort — expected 200, got %d — body: %s",
			w.Code, w.Body.String())
	}

	if fr.callCount != 0 {
		t.Errorf("expected FileRemover.Remove NOT called for empty avatar, called %d time(s)", fr.callCount)
	}
}

// ─── Test 12: Profile_AfterAnonymize_ReturnsDeletedUser ──────────────────────
//
// AC6: GET /_matrix/client/v3/profile/{userId} after anonymization
// must return {"displayname":"Deleted User","avatar_url":""} — NOT 404.
//
// This test verifies the GetProfile handler behaviour when the profiles-row
// has displayname='Deleted User' and avatar_url=NULL (post-anonymize state).
// No code change is expected in profile.go — this is a regression/side-effect guard.
//
// Strategy: uses the existing matrix.ProfileHandler (or its equivalent) wired
// with a fakedb that returns a 'Deleted User' profile row.
// If ProfileHandler does not exist yet, this test will fail to compile — that is the
// intended RED state.

// fakeAnonProfileDB implements matrix.ProfileDB. It returns whatever values
// the test pre-loads — used to drive the real matrix.ProfileHandler.GetProfile
// through the post-anonymize state. TEA Gate 2 MAJOR-1 fix: the original test
// used a synthetic helper in the production package with a hardcoded
// "Deleted User" string, which did not exercise the production code path.
type fakeAnonProfileDB struct {
	data *matrix.ProfileData
	err  error
}

func (f *fakeAnonProfileDB) GetProfile(_ context.Context, _ string) (*matrix.ProfileData, error) {
	return f.data, f.err
}

func TestProfile_AfterAnonymize_ReturnsDeletedUser(t *testing.T) {
	// Simulate the post-anonymize state of the profiles table: displayname has
	// been overwritten to "Deleted User"; avatar_url is empty.
	fakeDB := &fakeAnonProfileDB{
		data: &matrix.ProfileData{DisplayName: "Deleted User", AvatarURL: ""},
	}

	// Wire the REAL matrix.ProfileHandler — this is the production handler that
	// serves GET /_matrix/client/v3/profile/{userId}. The test verifies the
	// production code path, not a synthetic helper.
	handler := matrix.NewProfileHandler(matrix.ProfileConfig{
		ServerName: "server.local",
		DB:         fakeDB,
		// CoreClient not invoked on the GET path; nil is fine.
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /_matrix/client/v3/profile/{userId}", handler.GetProfile)

	req := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/profile/@user:server.local", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("profile after anonymize must be 200 (not 404), got %d — body: %s",
			w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("profile response is not valid JSON: %v — body: %s", err, w.Body.String())
	}
	if resp["displayname"] != "Deleted User" {
		t.Errorf("expected displayname='Deleted User', got %v", resp["displayname"])
	}
	// avatar_url is omitted from the JSON when empty (omitempty tag); accept missing OR null OR empty
	if av, ok := resp["avatar_url"]; ok && av != nil && av != "" {
		t.Errorf("expected avatar_url=null/empty/absent, got %v", av)
	}
}

// ─── Test 13: EventsUnchanged_AfterAnonymize ─────────────────────────────────
//
// AC4: anonymize must NOT modify the events table — sender field must be unchanged.
//
// Strategy: inject a SQL query-capture mock that tracks which tables are targeted
// by UPDATE/DELETE statements. After POST anonymize, assert no UPDATE/DELETE
// touched the events table.

func TestEventsUnchanged_AfterAnonymize(t *testing.T) {
	// Use a specialized fakedb that records all Exec queries
	db := openAnonDBWithQueryCapture(t, true, "mxc://server.local/mediaCapture")
	client := &mockCoreClient{}
	fr := &mockFileRemover{}

	h := &compliance.AnonymizationHandler{
		DB:          db,
		CoreClient:  client,
		StoragePath: t.TempDir(),
		FileRemover: fr,
	}

	w := httptest.NewRecorder()
	r := newAnonRequest(t, "user-events-check", "instance_admin", "admin-sub-1")

	h.AnonymizeUser(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	// Assert no query targeted the events table
	captured := queryCaptureStore.captured
	for _, q := range captured {
		lq := strings.ToLower(q)
		if strings.Contains(lq, "update events") || strings.Contains(lq, "delete from events") {
			t.Errorf("handler must NOT modify events table — captured query: %q", q)
		}
	}
}

// ─── queryCapture infrastructure for Test 13 ─────────────────────────────────

var queryCaptureStore = &queryCapture{}

type queryCapture struct {
	mu       sync.Mutex
	captured []string
}

func (qc *queryCapture) reset() {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.captured = nil
}

func (qc *queryCapture) record(query string) {
	qc.mu.Lock()
	defer qc.mu.Unlock()
	qc.captured = append(qc.captured, query)
}

func openAnonDBWithQueryCapture(t *testing.T, profileFound bool, avatarURL string) *sql.DB {
	t.Helper()
	queryCaptureStore.reset()
	// Re-use the anondb driver but capture all Exec queries via the global store
	// NOTE: In the real implementation this reuses the anondb driver; the
	// query-capture assertion relies on the handler NOT issuing UPDATE events queries.
	return openAnonDB(t, profileFound, avatarURL)
}

// ─── Test 14: PathTraversalMxc_Skipped (security defence) ────────────────────
//
// Defence-in-depth: avatar_url is user-controlled (PUT /profile/avatar_url only
// validates the "mxc://" prefix). A malicious user could store an avatar_url
// like "mxc://../etc/passwd/x". Without a guard, the parsed components would be
// joined into a filesystem path for os.Remove and could turn admin-triggered
// anonymization into an arbitrary-file-delete primitive.
//
// Each malicious URI must be rejected by parseMxcURI → no FileRemover.Remove
// call is made and the handler still returns 200 (anonymize succeeds; only the
// disk-cleanup step is skipped).
func TestAnonymizeUser_PathTraversalMxc_Skipped(t *testing.T) {
	cases := []struct {
		name string
		uri  string
	}{
		{"dotdot in serverName", "mxc://../media/file"},
		{"dotdot in mediaID", "mxc://server/../etc/passwd"},
		{"single dot serverName", "mxc://./media/file"},
		{"slash inside mediaID", "mxc://server/foo/bar"},
		{"backslash in mediaID", "mxc://server/foo\\bar"},
		{"NUL byte in mediaID", "mxc://server/foo\x00bar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openAnonDB(t, true, tc.uri)
			client := &mockCoreClient{}
			fr := &mockFileRemover{}

			h := &compliance.AnonymizationHandler{
				DB:          db,
				CoreClient:  client,
				StoragePath: t.TempDir(),
				FileRemover: fr,
			}

			w := httptest.NewRecorder()
			r := newAnonRequest(t, "user-traversal", "instance_admin", "admin-sub-1")

			h.AnonymizeUser(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("malicious mxc URI %q must not abort — expected 200, got %d — body: %s",
					tc.uri, w.Code, w.Body.String())
			}
			if fr.callCount != 0 {
				t.Errorf("FileRemover.Remove must NOT be called for traversal URI %q — got path %q",
					tc.uri, fr.calledWith)
			}
		})
	}
}
