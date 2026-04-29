package compliance_test

// session_revoke_test.go — Story 5.29c: FB-E5-04 — Session Revoke Endpoint (AC2)
//
// RED-phase: ALL tests in this file FAIL until Story 5.29c is implemented.
// Failing reason: RevokeSessionHandler type and the admin endpoint
// POST /api/v1/admin/compliance/sessions/{id}/revoke do not exist yet.
//
// Test strategy:
//   - revokeSessionFakeDriver: minimal SQL driver encoded as DSN flags:
//       alreadyRevoked=<true|false>  — whether compliance_sessions row already has revoked_at
//       rowExists=<true|false>       — whether the session row exists
//   - Context keys ContextKeySub, ContextKeySystemRole injected directly.
//   - mockCoreClient (from handler_test.go) captures audit emissions.
//   - httptest.NewRecorder captures status codes and bodies.
//
// AC coverage:
//   AC2 — TestRevokeSession_HappyPath_Returns200
//   AC2 — TestRevokeSession_NonAdmin_Returns403
//   AC2 — TestRevokeSession_AlreadyRevoked_Idempotent_Returns200

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/compliance"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── revokeSessionFakeDriver ─────────────────────────────────────────────────

var revokeSessionDriverOnce sync.Once

func init() {
	revokeSessionDriverOnce.Do(func() {
		sql.Register("revokedb", &revokeSessionFakeDriver{})
	})
}

type revokeSessionFakeDriver struct{}

func (d *revokeSessionFakeDriver) Open(name string) (driver.Conn, error) {
	return &revokeSessionFakeConn{dsn: name}, nil
}

type revokeSessionFakeConn struct{ dsn string }

func (c *revokeSessionFakeConn) dsnFlag(key string) string {
	for _, part := range strings.Split(c.dsn, ";") {
		if strings.HasPrefix(part, key+"=") {
			return strings.TrimPrefix(part, key+"=")
		}
	}
	return ""
}

func (c *revokeSessionFakeConn) Prepare(query string) (driver.Stmt, error) {
	rowExists := c.dsnFlag("rowExists") != "false" // default true
	alreadyRevoked := c.dsnFlag("alreadyRevoked") == "true"
	return &revokeSessionFakeStmt{
		query:          query,
		rowExists:      rowExists,
		alreadyRevoked: alreadyRevoked,
	}, nil
}

func (c *revokeSessionFakeConn) Close() error                        { return nil }
func (c *revokeSessionFakeConn) Begin() (driver.Tx, error)           { return &revokeFakeTx{}, nil }

type revokeFakeTx struct{}

func (t *revokeFakeTx) Commit() error   { return nil }
func (t *revokeFakeTx) Rollback() error { return nil }

type revokeSessionFakeStmt struct {
	query          string
	rowExists      bool
	alreadyRevoked bool
}

func (s *revokeSessionFakeStmt) Close() error  { return nil }
func (s *revokeSessionFakeStmt) NumInput() int { return -1 }

func (s *revokeSessionFakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	// UPDATE compliance_sessions SET revoked_at = NOW() WHERE id = $1 RETURNING id
	if strings.Contains(s.query, "revoked_at") && strings.Contains(s.query, "UPDATE") {
		if !s.rowExists {
			return driver.RowsAffected(0), nil
		}
		return driver.RowsAffected(1), nil
	}
	return driver.RowsAffected(1), nil
}

func (s *revokeSessionFakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	// SELECT id, revoked_at FROM compliance_sessions WHERE id = $1
	if strings.Contains(s.query, "compliance_sessions") && strings.Contains(s.query, "SELECT") {
		if !s.rowExists {
			return &revokeFakeRows{cols: []string{"id", "revoked_at"}, data: nil}, nil
		}
		var revokedAt interface{}
		if s.alreadyRevoked {
			revokedAt = time.Now().Add(-time.Hour) // was revoked 1h ago
		}
		return &revokeFakeRows{
			cols: []string{"id", "revoked_at"},
			data: [][]driver.Value{{"session-uuid-001", revokedAt}},
		}, nil
	}
	// UPDATE ... RETURNING id
	if strings.Contains(s.query, "UPDATE") && strings.Contains(s.query, "RETURNING") {
		if !s.rowExists {
			return &revokeFakeRows{cols: []string{"id"}, data: nil}, nil
		}
		return &revokeFakeRows{
			cols: []string{"id"},
			data: [][]driver.Value{{"session-uuid-001"}},
		}, nil
	}
	return &revokeFakeRows{}, nil
}

type revokeFakeRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *revokeFakeRows) Columns() []string { return r.cols }
func (r *revokeFakeRows) Close() error      { return nil }
func (r *revokeFakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func openRevokeDB(t *testing.T, flags string) *sql.DB {
	t.Helper()
	db, err := sql.Open("revokedb", flags)
	if err != nil {
		t.Fatalf("sql.Open(revokedb): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newRevokeSessionHandler constructs a RevokeSessionHandler.
// FAILS to compile until compliance.RevokeSessionHandler exists (red phase).
func newRevokeSessionHandler(t *testing.T, db *sql.DB, client *mockCoreClient) *compliance.RevokeSessionHandler {
	t.Helper()
	return &compliance.RevokeSessionHandler{
		DB:         db,
		CoreClient: client,
	}
}

func newRevokeRequest(t *testing.T, sessionID, systemRole, sub string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost,
		"/api/v1/admin/compliance/sessions/"+sessionID+"/revoke", http.NoBody)
	r.SetPathValue("sessionId", sessionID)
	ctx := r.Context()
	ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, systemRole)
	ctx = context.WithValue(ctx, middleware.ContextKeySub, sub)
	return r.WithContext(ctx)
}

// ─── Test 1: HappyPath → 200, revoked_at set, audit emitted ─────────────────
//
// AC2
//
// Given: instance_admin role, valid sessionId, session exists and is active
// When: POST /api/v1/admin/compliance/sessions/{id}/revoke
// Then: HTTP 200, DB row has revoked_at set, audit compliance_session_revoked emitted

func TestRevokeSession_HappyPath_Returns200(t *testing.T) {
	sessionID := "550e8400-e29b-41d4-a716-revoke000001"
	db := openRevokeDB(t, "rowExists=true;alreadyRevoked=false")
	client := &mockCoreClient{}
	h := newRevokeSessionHandler(t, db, client)

	w := httptest.NewRecorder()
	r := newRevokeRequest(t, sessionID, "instance_admin", "@admin:server.example")

	h.RevokeSession(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	// Audit must have been emitted with compliance_session_revoked
	if client.callCount() == 0 {
		t.Fatal("audit WriteAuditLog was not called on revoke happy path")
	}
	req := client.lastReceived()
	if req == nil {
		t.Fatal("lastReceived audit request is nil")
	}
	if req.Action != "compliance_session_revoked" {
		t.Errorf("expected audit action=compliance_session_revoked, got %q", req.Action)
	}
	if req.TargetId != sessionID {
		t.Errorf("expected audit target_id=%q, got %q", sessionID, req.TargetId)
	}
}

// ─── Test 2: NonAdmin → 403 ──────────────────────────────────────────────────
//
// AC2 (role gate)
//
// Given: compliance_officer role (not instance_admin)
// When: POST /api/v1/admin/compliance/sessions/{id}/revoke
// Then: HTTP 403 M_FORBIDDEN — only instance_admin may revoke sessions

func TestRevokeSession_NonAdmin_Returns403(t *testing.T) {
	sessionID := "550e8400-e29b-41d4-a716-revoke000002"
	db := openRevokeDB(t, "rowExists=true;alreadyRevoked=false")
	h := newRevokeSessionHandler(t, db, &mockCoreClient{})

	w := httptest.NewRecorder()
	r := newRevokeRequest(t, sessionID, "compliance_officer", "@officer:server.example")

	h.RevokeSession(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-admin, got %d — body: %s", w.Code, w.Body.String())
	}
}

// ─── Test 3: AlreadyRevoked → 200 idempotent ─────────────────────────────────
//
// AC2 (idempotency)
//
// Given: instance_admin role, sessionId that already has revoked_at set
// When: POST /api/v1/admin/compliance/sessions/{id}/revoke (second call)
// Then: HTTP 200 (idempotent — revoke is not an error if already revoked)

func TestRevokeSession_AlreadyRevoked_Idempotent_Returns200(t *testing.T) {
	sessionID := "550e8400-e29b-41d4-a716-revoke000003"
	db := openRevokeDB(t, "rowExists=true;alreadyRevoked=true")
	h := newRevokeSessionHandler(t, db, &mockCoreClient{})

	w := httptest.NewRecorder()
	r := newRevokeRequest(t, sessionID, "instance_admin", "@admin:server.example")

	h.RevokeSession(w, r)

	// Idempotent: already revoked should still return 200 (not 409 or 400).
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK (idempotent revoke), got %d — body: %s", w.Code, w.Body.String())
	}
}
