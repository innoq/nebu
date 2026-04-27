package compliance_test

// session_test.go — Story 5.5: Compliance Session Handler — RED-phase tests
//
// ALL tests in this file are expected to FAIL until Story 5.5 is implemented.
// Failing reason: SessionHandler type and PostSession method do not exist yet.
//
// Test strategy:
//   - sessionFakeDB: a third sql driver registered as "sessiondb".
//     DSN encodes scenario flags:
//       requesterSub=<value>    — requester_user_id in compliance_requests row
//       requestStatus=<value>   — status column in compliance_requests row
//       rowExists=<true|false>  — whether SELECT on compliance_requests returns a row
//       activeSession=<true|false> — whether compliance_sessions has an active (non-revoked) row
//   - mockCoreClient is already declared in handler_test.go (same package) — reused.
//   - Context keys (ContextKeySub, ContextKeySystemRole) injected directly — no JWT middleware.
//   - httptest.NewRecorder captures status and body for assertions.
//   - SessionHandler.SigningKey is a test-generated Ed25519 key pair.
//
// AC coverage:
//   AC1  — TestPostSession_HappyPath
//   AC2  — TestPostSession_CallerNotRequester_Returns403
//   AC3  — TestPostSession_StatusPending_Returns403, TestPostSession_StatusRejected_Returns403
//   AC4  — TestPostSession_UnknownRequest_Returns404
//   AC5  — TestPostSession_DuplicateActiveSession_Returns409
//   AC6  — TestPostSession_NonOfficer_Returns403
//   AC7  — TestPostSession_AuditEmittedWithExpiresAt, TestPostSession_AuditFailure_Still201
//   AC8  — (jwt_test.go)
//   AC9  — (session_expiry_worker_test.exs)
//   AC10 — (integration test)
//   AC11 — seeding not unit-tested (startup code)
//   AC12 — TestPostSession_PathParamLengthCap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	josejwt "github.com/go-jose/go-jose/v4/jwt"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/nebu/nebu/internal/compliance"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Test Ed25519 keypair ─────────────────────────────────────────────────────

var (
	testSessionPub  ed25519.PublicKey
	testSessionPriv ed25519.PrivateKey
	testKeyOnce     sync.Once
)

func initTestSessionKey() {
	testKeyOnce.Do(func() {
		var err error
		testSessionPub, testSessionPriv, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			panic("session_test: failed to generate Ed25519 key pair: " + err.Error())
		}
	})
}

// ─── sessionFakeDriver ────────────────────────────────────────────────────────
//
// A third sql driver registered as "sessiondb" to avoid collision with
// "fakedb" (handler_test.go) and "approvaldb" (approval_test.go).
//
// DSN encoding (all flags as key=value, separated by ";"):
//   requesterSub=<string>      — requester_user_id in mock compliance_requests row
//   requestStatus=<string>     — status in mock compliance_requests row ("approved"|"pending"|"rejected")
//   rowExists=<true|false>     — whether compliance_requests SELECT returns a row
//   activeSession=<true|false> — whether compliance_sessions SELECT returns a row (active session check)

var sessionDriverOnce sync.Once

func init() {
	sessionDriverOnce.Do(func() {
		sql.Register("sessiondb", &sessionFakeDriver{})
	})
}

type sessionFakeDriver struct{}

func (d *sessionFakeDriver) Open(name string) (driver.Conn, error) {
	return &sessionFakeConn{dsn: name}, nil
}

type sessionFakeConn struct {
	dsn string
}

func (c *sessionFakeConn) dsnFlag(key string) string {
	for _, part := range strings.Split(c.dsn, ";") {
		if strings.HasPrefix(part, key+"=") {
			return strings.TrimPrefix(part, key+"=")
		}
	}
	return ""
}

func (c *sessionFakeConn) Prepare(query string) (driver.Stmt, error) {
	requesterSub := c.dsnFlag("requesterSub")
	requestStatus := c.dsnFlag("requestStatus")
	if requestStatus == "" {
		requestStatus = "approved"
	}
	rowExists := c.dsnFlag("rowExists") != "false" // default true
	activeSession := c.dsnFlag("activeSession") == "true"

	return &sessionFakeStmt{
		query:         query,
		requesterSub:  requesterSub,
		requestStatus: requestStatus,
		rowExists:     rowExists,
		activeSession: activeSession,
	}, nil
}

func (c *sessionFakeConn) Close() error { return nil }
func (c *sessionFakeConn) Begin() (driver.Tx, error) {
	return &sessionFakeTx{}, nil
}

type sessionFakeTx struct{}

func (t *sessionFakeTx) Commit() error   { return nil }
func (t *sessionFakeTx) Rollback() error { return nil }

type sessionFakeStmt struct {
	query         string
	requesterSub  string
	requestStatus string
	rowExists     bool
	activeSession bool
}

func (s *sessionFakeStmt) Close() error  { return nil }
func (s *sessionFakeStmt) NumInput() int { return -1 } // variadic

func (s *sessionFakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}

func (s *sessionFakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	// SELECT requester_user_id, status, room_id, time_range_start::text, time_range_end::text
	// FROM compliance_requests WHERE id = $1  (consolidated 5-column pre-flight query)
	if strings.Contains(s.query, "compliance_requests") && strings.Contains(s.query, "requester_user_id") && strings.Contains(s.query, "status") {
		cols := []string{"requester_user_id", "status", "room_id", "time_range_start", "time_range_end"}
		if !s.rowExists {
			return &sessionFakeRows{cols: cols, data: nil}, nil
		}
		return &sessionFakeRows{
			cols: cols,
			data: [][]driver.Value{{
				s.requesterSub,
				s.requestStatus,
				"!test-room:server.example",
				"2026-01-01T00:00:00Z",
				"2026-01-02T00:00:00Z",
			}},
		}, nil
	}

	// SELECT 1 FROM compliance_sessions WHERE request_id = $1 AND revoked_at IS NULL
	if strings.Contains(s.query, "compliance_sessions") && strings.Contains(s.query, "revoked_at IS NULL") {
		if s.activeSession {
			return &sessionFakeRows{cols: []string{"?column?"}, data: [][]driver.Value{{int64(1)}}}, nil
		}
		return &sessionFakeRows{cols: []string{"?column?"}, data: nil}, nil
	}

	// INSERT INTO compliance_sessions (...) RETURNING id, expires_at
	if strings.Contains(s.query, "INSERT INTO compliance_sessions") {
		fakeID := "660e8400-e29b-41d4-a716-446655440000"
		fakeExpiresAt := time.Now().Add(86400 * time.Second).Format(time.RFC3339)
		return &sessionFakeRows{
			cols: []string{"id", "expires_at"},
			data: [][]driver.Value{{fakeID, fakeExpiresAt}},
		}, nil
	}

	return &sessionFakeRows{}, nil
}

type sessionFakeRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *sessionFakeRows) Columns() []string { return r.cols }
func (r *sessionFakeRows) Close() error      { return nil }
func (r *sessionFakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// openSessionDB opens a sessiondb with the provided DSN flags.
// flags is a semicolon-separated list of key=value pairs.
func openSessionDB(t *testing.T, flags string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sessiondb", flags)
	if err != nil {
		t.Fatalf("sql.Open(sessiondb): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newSessionHandler constructs a SessionHandler with the test key pair.
// Fails the test if SessionHandler type does not exist (expected in red phase).
func newSessionHandler(t *testing.T, db *sql.DB, client *mockCoreClient) *compliance.SessionHandler {
	t.Helper()
	initTestSessionKey()
	return &compliance.SessionHandler{
		DB:         db,
		CoreClient: client,
		SigningKey: testSessionPriv,
		PublicKey:  testSessionPub,
	}
}

// newSessionRequest builds a POST request to the session endpoint with context values injected.
func newSessionRequest(t *testing.T, requestID, systemRole, sub string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost,
		"/api/v1/compliance/access-requests/"+requestID+"/session", http.NoBody)

	// Simulate go net/http PathValue — inject via context equivalent for unit test
	r.SetPathValue("requestId", requestID)

	ctx := r.Context()
	ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, systemRole)
	ctx = context.WithValue(ctx, middleware.ContextKeySub, sub)
	return r.WithContext(ctx)
}

// ─── Test 1: HappyPath → 201 + JWT ────────────────────────────────────────────
//
// AC1
//
// Given: compliance_officer, mock DB row requester=callerSub, status=approved, no active session
// When: POST .../session
// Then: HTTP 201, body has session_token (parseable JWT) and expires_at (RFC3339)
//       JWT claims: sub=callerSub, compliance_request_id, room_id, time_range_start/end,
//       exp≈now+86400, iat≈now
//       DB has a row in compliance_sessions (INSERT was called)
//       Audit compliance_session_issued was emitted

func TestPostSession_HappyPath(t *testing.T) {
	initTestSessionKey()
	callerSub := "@alice:server.example"
	requestID := "550e8400-e29b-41d4-a716-446655440000"

	// DB: row exists, status=approved, requesterSub==callerSub, no active session
	db := openSessionDB(t, "requesterSub="+callerSub+";requestStatus=approved;rowExists=true;activeSession=false")
	client := &mockCoreClient{}
	h := newSessionHandler(t, db, client)

	w := httptest.NewRecorder()
	r := newSessionRequest(t, requestID, "compliance_officer", callerSub)

	h.PostSession(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v — body: %s", err, w.Body.String())
	}

	tokenStr, ok := resp["session_token"]
	if !ok || tokenStr == "" {
		t.Fatal("response missing session_token field")
	}
	expiresAtStr, ok := resp["expires_at"]
	if !ok || expiresAtStr == "" {
		t.Fatal("response missing expires_at field")
	}

	// expires_at must be RFC3339 parseable
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		t.Errorf("expires_at is not RFC3339: %q — %v", expiresAtStr, err)
	}
	// expires_at should be approximately now+86400 (within ±60s tolerance)
	expectedExp := time.Now().Add(86400 * time.Second)
	diff := expiresAt.Sub(expectedExp)
	if diff < -60*time.Second || diff > 60*time.Second {
		t.Errorf("expires_at %v is not approximately now+86400s (got diff %v)", expiresAt, diff)
	}

	// Parse and verify JWT claims using go-jose/v4
	tok, err := jose.ParseSigned(tokenStr, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil {
		t.Fatalf("session_token is not a valid JWS: %v", err)
	}
	rawPayload, err := tok.Verify(testSessionPub)
	if err != nil {
		t.Fatalf("JWT signature verification failed: %v", err)
	}

	var claims compliance.ComplianceClaims
	if err := json.Unmarshal(rawPayload, &claims); err != nil {
		t.Fatalf("JWT payload is not valid JSON: %v — raw: %s", err, string(rawPayload))
	}

	if claims.Sub != callerSub {
		t.Errorf("JWT sub: expected %q, got %q", callerSub, claims.Sub)
	}
	if claims.ComplianceRequestID == "" {
		t.Error("JWT compliance_request_id must not be empty")
	}
	// exp ≈ now + 86400
	expTime := time.Unix(claims.Exp, 0)
	expDiff := expTime.Sub(expectedExp)
	if expDiff < -60*time.Second || expDiff > 60*time.Second {
		t.Errorf("JWT exp %v is not approximately now+86400s", expTime)
	}
	// iat ≈ now
	iatTime := time.Unix(claims.Iat, 0)
	iatDiff := time.Since(iatTime)
	if iatDiff < 0 || iatDiff > 10*time.Second {
		t.Errorf("JWT iat %v is not approximately now (diff %v)", iatTime, iatDiff)
	}

	// Audit must have been emitted
	if client.callCount() == 0 {
		t.Fatal("audit WriteAuditLog was not called on happy path")
	}
}

// ─── Test 2: CallerNotRequester → 403 ────────────────────────────────────────
//
// AC2
//
// Given: compliance_officer, callerSub=@bob:server, DB row requesterSub=@alice:server, status=approved
// When: POST .../session
// Then: HTTP 403 M_FORBIDDEN "Only the original requester can issue a session"

func TestPostSession_CallerNotRequester_Returns403(t *testing.T) {
	callerSub := "@bob:server.example"
	requesterSub := "@alice:server.example"
	requestID := "550e8400-e29b-41d4-a716-446655440001"

	db := openSessionDB(t, "requesterSub="+requesterSub+";requestStatus=approved;rowExists=true;activeSession=false")
	h := newSessionHandler(t, db, &mockCoreClient{})

	w := httptest.NewRecorder()
	r := newSessionRequest(t, requestID, "compliance_officer", callerSub)

	h.PostSession(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode=M_FORBIDDEN, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "Only the original requester") {
		t.Errorf("expected error to mention 'Only the original requester', got %q", resp["error"])
	}
}

// ─── Test 3: StatusPending → 403 ─────────────────────────────────────────────
//
// AC3
//
// Given: compliance_officer, callerSub==requesterSub, status=pending
// When: POST .../session
// Then: HTTP 403 M_FORBIDDEN "Request must be in approved status"

func TestPostSession_StatusPending_Returns403(t *testing.T) {
	callerSub := "@alice:server.example"
	requestID := "550e8400-e29b-41d4-a716-446655440002"

	db := openSessionDB(t, "requesterSub="+callerSub+";requestStatus=pending;rowExists=true;activeSession=false")
	h := newSessionHandler(t, db, &mockCoreClient{})

	w := httptest.NewRecorder()
	r := newSessionRequest(t, requestID, "compliance_officer", callerSub)

	h.PostSession(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for pending status, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode=M_FORBIDDEN, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "approved status") {
		t.Errorf("expected error to mention 'approved status', got %q", resp["error"])
	}
}

// ─── Test 4: StatusRejected → 403 ────────────────────────────────────────────
//
// AC3
//
// Given: compliance_officer, callerSub==requesterSub, status=rejected
// When: POST .../session
// Then: HTTP 403 M_FORBIDDEN "Request must be in approved status"

func TestPostSession_StatusRejected_Returns403(t *testing.T) {
	callerSub := "@alice:server.example"
	requestID := "550e8400-e29b-41d4-a716-446655440003"

	db := openSessionDB(t, "requesterSub="+callerSub+";requestStatus=rejected;rowExists=true;activeSession=false")
	h := newSessionHandler(t, db, &mockCoreClient{})

	w := httptest.NewRecorder()
	r := newSessionRequest(t, requestID, "compliance_officer", callerSub)

	h.PostSession(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for rejected status, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode=M_FORBIDDEN, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "approved status") {
		t.Errorf("expected error to mention 'approved status', got %q", resp["error"])
	}
}

// ─── Test 5: DuplicateActiveSession → 409 ────────────────────────────────────
//
// AC5
//
// Given: compliance_officer, callerSub==requesterSub, status=approved, active session EXISTS
// When: POST .../session
// Then: HTTP 409 M_CONFLICT "An active session already exists for this request"

func TestPostSession_DuplicateActiveSession_Returns409(t *testing.T) {
	callerSub := "@alice:server.example"
	requestID := "550e8400-e29b-41d4-a716-446655440004"

	db := openSessionDB(t, "requesterSub="+callerSub+";requestStatus=approved;rowExists=true;activeSession=true")
	h := newSessionHandler(t, db, &mockCoreClient{})

	w := httptest.NewRecorder()
	r := newSessionRequest(t, requestID, "compliance_officer", callerSub)

	h.PostSession(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_CONFLICT" {
		t.Errorf("expected errcode=M_CONFLICT, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "active session already exists") {
		t.Errorf("expected error to mention 'active session already exists', got %q", resp["error"])
	}
}

// ─── Test 6: UnknownRequest → 404 ────────────────────────────────────────────
//
// AC4
//
// Given: compliance_officer, DB SELECT returns 0 rows (rowExists=false)
// When: POST .../session
// Then: HTTP 404 M_NOT_FOUND "Request not found"

func TestPostSession_UnknownRequest_Returns404(t *testing.T) {
	callerSub := "@alice:server.example"
	requestID := "550e8400-e29b-41d4-a716-446655440005"

	db := openSessionDB(t, "rowExists=false")
	h := newSessionHandler(t, db, &mockCoreClient{})

	w := httptest.NewRecorder()
	r := newSessionRequest(t, requestID, "compliance_officer", callerSub)

	h.PostSession(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d — body: %s", w.Code, w.Body.String())
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

// ─── Test 7: NonOfficer → 403 ────────────────────────────────────────────────
//
// AC6
//
// Given: valid JWT with system_role=instance_admin (not compliance_officer)
// When: POST .../session
// Then: HTTP 403 M_FORBIDDEN "Compliance officer role required"

func TestPostSession_NonOfficer_Returns403(t *testing.T) {
	callerSub := "@alice:server.example"
	requestID := "550e8400-e29b-41d4-a716-446655440006"

	db := openSessionDB(t, "requesterSub="+callerSub+";requestStatus=approved;rowExists=true;activeSession=false")
	h := newSessionHandler(t, db, &mockCoreClient{})

	w := httptest.NewRecorder()
	r := newSessionRequest(t, requestID, "instance_admin", callerSub) // wrong role

	h.PostSession(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-officer, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_FORBIDDEN" {
		t.Errorf("expected errcode=M_FORBIDDEN, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "Compliance officer role required") {
		t.Errorf("expected error to mention 'Compliance officer role required', got %q", resp["error"])
	}
}

// ─── Test 8: AuditEmittedWithExpiresAt ───────────────────────────────────────
//
// AC7
//
// Given: happy path conditions
// When: POST .../session
// Then: WriteAuditLog called once with action=compliance_session_issued,
//       target_type=compliance_request, target_id=requestId,
//       metadata_json contains expires_at as RFC3339 string

func TestPostSession_AuditEmittedWithExpiresAt(t *testing.T) {
	callerSub := "@alice:server.example"
	requestID := "550e8400-e29b-41d4-a716-446655440007"

	db := openSessionDB(t, "requesterSub="+callerSub+";requestStatus=approved;rowExists=true;activeSession=false")
	client := &mockCoreClient{}
	h := newSessionHandler(t, db, client)

	w := httptest.NewRecorder()
	r := newSessionRequest(t, requestID, "compliance_officer", callerSub)

	h.PostSession(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("handler must return 201 for audit test to be meaningful, got %d — body: %s", w.Code, w.Body.String())
	}

	if client.callCount() == 0 {
		t.Fatal("audit WriteAuditLog was NOT called")
	}

	req := client.lastReceived()
	if req == nil {
		t.Fatal("lastReceived audit request is nil")
	}
	if req.Action != "compliance_session_issued" {
		t.Errorf("expected action=compliance_session_issued, got %q", req.Action)
	}
	if req.TargetType != "compliance_request" {
		t.Errorf("expected target_type=compliance_request, got %q", req.TargetType)
	}
	if req.TargetId != requestID {
		t.Errorf("expected target_id=%q, got %q", requestID, req.TargetId)
	}
	if req.ActorUserId != callerSub {
		t.Errorf("expected actor_user_id=%q, got %q", callerSub, req.ActorUserId)
	}
	if req.Outcome != "success" {
		t.Errorf("expected outcome=success, got %q", req.Outcome)
	}

	// metadata_json must contain expires_at as RFC3339 string
	var meta map[string]any
	if err := json.Unmarshal(req.MetadataJson, &meta); err != nil {
		t.Fatalf("metadata_json is not valid JSON: %v — raw: %q", err, string(req.MetadataJson))
	}
	expiresAt, ok := meta["expires_at"].(string)
	if !ok || expiresAt == "" {
		t.Errorf("metadata.expires_at missing or not a string — got %v (type %T)", meta["expires_at"], meta["expires_at"])
	}
	// Must parse as RFC3339
	if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
		t.Errorf("metadata.expires_at %q is not RFC3339: %v", expiresAt, err)
	}
}

// ─── Test 9: AuditFailure → Still 201 ────────────────────────────────────────
//
// AC7 never-raise policy
//
// Given: happy path conditions, audit gRPC call fails
// When: POST .../session
// Then: HTTP 201 — audit failure must not block the response

func TestPostSession_AuditFailure_Still201(t *testing.T) {
	callerSub := "@alice:server.example"
	requestID := "550e8400-e29b-41d4-a716-446655440008"
	db := openSessionDB(t, "requesterSub="+callerSub+";requestStatus=approved;rowExists=true;activeSession=false")
	client := &mockCoreClient{failWith: errSimulatedAuditFailure}
	h := newSessionHandler(t, db, client)

	w := httptest.NewRecorder()
	r := newSessionRequest(t, requestID, "compliance_officer", callerSub)

	h.PostSession(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("audit failure must not block 201 — got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if _, ok := resp["session_token"]; !ok {
		t.Error("response must contain session_token even when audit fails")
	}
}

// errSimulatedAuditFailure is a package-level sentinel for the never-raise audit test.
// Declared here to avoid redeclaring in handler_test.go (same package — already has fmt.Errorf).
var errSimulatedAuditFailure = simulatedError("simulated gRPC audit failure for session test")

type simulatedError string

func (e simulatedError) Error() string { return string(e) }

// ─── Test 10: PathParamLengthCap → 400 ───────────────────────────────────────
//
// AC12 (defensive — 256+ byte requestId)
//
// Given: compliance_officer, requestId is a 300-byte string
// When: POST .../session
// Then: HTTP 400 M_BAD_JSON or M_FORBIDDEN before DB is ever hit

func TestPostSession_PathParamLengthCap(t *testing.T) {
	callerSub := "@alice:server.example"
	// 300-byte pathological requestId
	longID := strings.Repeat("x", 300)

	db := openSessionDB(t, "requesterSub="+callerSub+";requestStatus=approved;rowExists=true;activeSession=false")
	h := newSessionHandler(t, db, &mockCoreClient{})

	w := httptest.NewRecorder()
	r := newSessionRequest(t, longID, "compliance_officer", callerSub)

	h.PostSession(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for pathological requestId, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] == "" {
		t.Errorf("expected non-empty errcode for pathological requestId, got empty — body: %s", w.Body.String())
	}
}

// ─── compile-time import guard ────────────────────────────────────────────────
// These imports will fail to compile if go-jose/v4 is unavailable.
// The test intentionally references jose and josejwt to ensure the correct
// library is in use, even though the JWT validation tests are in jwt_test.go.
var _ = jose.EdDSA
var _ josejwt.Claims
