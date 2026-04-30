//go:build integration

package integration_test

// Story 7-16c — Audit-log integration smoke tests.
//
// TestAdminLogout_EmitsAuditEntry: Full HTTP flow — DB-seeded admin session,
// forged HMAC-signed cookie, CSRF dance, POST /admin/logout, audit_log DB verification.
//
// TestAdminLogin_EmitsAuditEntry, TestAdminLoginFailure_EmitsAuditEntry,
// TestBootstrap_EmitsAuditEntry: Direct gRPC WriteAuditLog call to the Elixir core,
// then verify the row appears in the audit_log table. Tests the gRPC → Elixir → DB
// pipeline end-to-end. Handler-level behavior (CallbackHandler / ClaimSelectionHandler
// calling logAuditEvent) is covered by unit tests in
// gateway/internal/admin/auth_audit_test.go.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ─── Helpers ──────────────────────────────────────────────────────────────────

// auditSIDCookie mirrors adminSessionSIDCookie for JSON marshalling in forgeAdminSIDCookie.
type auditSIDCookie struct {
	SID string `json:"sid"`
}

// seedAdminSessionForAudit inserts a row into admin_sessions via the migration DB role.
func seedAdminSessionForAudit(t *testing.T, sid, userID string) {
	t.Helper()
	db, err := sql.Open("pgx", migrationDBURL)
	if err != nil {
		t.Fatalf("seedAdminSessionForAudit: open db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT INTO admin_sessions (sid, user_id, created_at, expires_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (sid) DO UPDATE SET expires_at = EXCLUDED.expires_at`,
		sid, userID, time.Now(), time.Now().Add(2*time.Hour),
	)
	if err != nil {
		t.Fatalf("seedAdminSessionForAudit: insert: %v", err)
	}
}

// forgeAdminSIDCookie creates an HMAC-signed admin_session cookie with payload {"sid": sid}.
// Mirrors AdminAuth.signCookie / signTestCookie using internalSecret.
func forgeAdminSIDCookie(sid string) string {
	payload, _ := json.Marshal(auditSIDCookie{SID: sid})
	return signTestCookie([]byte(strings.TrimSpace(internalSecret)), payload)
}

// getCSRFTokenWithSession makes a GET to the given admin path with the admin_session cookie
// and returns the csrf_token cookie value issued by the server.
func getCSRFTokenWithSession(t *testing.T, path, sessionCookie string) string {
	t.Helper()
	client := noRedirectClient()
	req, err := http.NewRequest(http.MethodGet, adminURL(path), nil)
	if err != nil {
		t.Fatalf("getCSRFTokenWithSession: build request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: sessionCookie})
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("getCSRFTokenWithSession: GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == "csrf_token" {
			return c.Value
		}
	}
	t.Fatalf("getCSRFTokenWithSession: no csrf_token cookie in response to GET %s (status=%d)", path, resp.StatusCode)
	return ""
}

// dialCoreGRPCForAudit connects to the Elixir core gRPC server (coreGRPCAddr)
// with the PSK node token injected via UnaryInterceptor metadata.
func dialCoreGRPCForAudit(t *testing.T) pb.CoreServiceClient {
	t.Helper()
	secret := strings.TrimSpace(internalSecret)
	authInterceptor := func(
		ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			md = metadata.New(nil)
		}
		md.Set("x-nebu-node-token", secret)
		return invoker(metadata.NewOutgoingContext(ctx, md), method, req, reply, cc, opts...)
	}
	conn, err := grpc.NewClient(coreGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(authInterceptor),
	)
	if err != nil {
		t.Fatalf("dialCoreGRPCForAudit: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewCoreServiceClient(conn)
}

// countAuditLogRows returns the number of audit_log rows matching action and actor_user_id
// created strictly after the given timestamp.
func countAuditLogRows(t *testing.T, action, actorUserID string, after time.Time) int {
	t.Helper()
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		t.Fatalf("countAuditLogRows: open db: %v", err)
	}
	defer db.Close()
	var count int
	err = db.QueryRow(
		`SELECT COUNT(*) FROM audit_log
		 WHERE action = $1 AND actor_user_id = $2 AND event_time > $3`,
		action, actorUserID, after,
	).Scan(&count)
	if err != nil {
		t.Fatalf("countAuditLogRows: query: %v", err)
	}
	return count
}

// ─── Test: TestAdminLogout_EmitsAuditEntry ────────────────────────────────────
//
// Full HTTP flow: seed admin_sessions row → forge HMAC-signed SID cookie →
// GET /admin/dashboard to obtain csrf_token cookie → POST /admin/logout with both
// cookies and _csrf form field → verify audit_log row exists.

func TestAdminLogout_EmitsAuditEntry(t *testing.T) {
	testUserID := fmt.Sprintf("audit-logout-%d@example.com", time.Now().UnixNano())
	testSID := fmt.Sprintf("audit-logout-sid-%d", time.Now().UnixNano())

	seedAdminSessionForAudit(t, testSID, testUserID)
	sessionCookie := forgeAdminSIDCookie(testSID)
	csrfToken := getCSRFTokenWithSession(t, "/admin/dashboard", sessionCookie)

	before := time.Now()

	client := noRedirectClient()
	body := url.Values{"_csrf": {csrfToken}}.Encode()
	req, err := http.NewRequest(http.MethodPost, adminURL("/admin/logout"), strings.NewReader(body))
	if err != nil {
		t.Fatalf("build logout request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: sessionCookie})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /admin/logout: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 SeeOther from /admin/logout, got %d", resp.StatusCode)
	}

	// logAuditEvent is synchronous in LogoutHandler; poll briefly for DB propagation via gRPC.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if countAuditLogRows(t, "admin_logout", testUserID, before) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("admin_logout audit entry not found for user %q within 3s", testUserID)
}

// ─── Test: TestAdminLogin_EmitsAuditEntry ─────────────────────────────────────
//
// Direct gRPC WriteAuditLog → Elixir core → audit_log DB verification.
// Proves the gRPC → core → DB pipeline for admin_login events.

func TestAdminLogin_EmitsAuditEntry(t *testing.T) {
	client := dialCoreGRPCForAudit(t)
	actorID := fmt.Sprintf("audit-login-%d@example.com", time.Now().UnixNano())
	before := time.Now()

	resp, err := client.WriteAuditLog(context.Background(), &pb.WriteAuditLogRequest{
		ActorUserId: actorID,
		Action:      "admin_login",
		TargetType:  "user",
		TargetId:    actorID,
		Outcome:     "success",
	})
	if err != nil {
		t.Fatalf("WriteAuditLog(admin_login): %v", err)
	}
	if !resp.Ok {
		t.Fatal("WriteAuditLog(admin_login): core returned ok=false")
	}

	if countAuditLogRows(t, "admin_login", actorID, before) == 0 {
		t.Error("admin_login audit entry not found in audit_log after WriteAuditLog gRPC call")
	}
}

// ─── Test: TestAdminLoginFailure_EmitsAuditEntry ──────────────────────────────
//
// Direct gRPC WriteAuditLog → DB verification for admin_login_failed events.

func TestAdminLoginFailure_EmitsAuditEntry(t *testing.T) {
	client := dialCoreGRPCForAudit(t)
	actorID := fmt.Sprintf("audit-login-failed-%d@example.com", time.Now().UnixNano())
	before := time.Now()

	resp, err := client.WriteAuditLog(context.Background(), &pb.WriteAuditLogRequest{
		ActorUserId: actorID,
		Action:      "admin_login_failed",
		TargetType:  "user",
		TargetId:    actorID,
		Outcome:     "failure",
		ErrorDetail: "role_check_failed",
	})
	if err != nil {
		t.Fatalf("WriteAuditLog(admin_login_failed): %v", err)
	}
	if !resp.Ok {
		t.Fatal("WriteAuditLog(admin_login_failed): core returned ok=false")
	}

	if countAuditLogRows(t, "admin_login_failed", actorID, before) == 0 {
		t.Error("admin_login_failed audit entry not found in audit_log after WriteAuditLog gRPC call")
	}
}

// ─── Test: TestBootstrap_EmitsAuditEntry ─────────────────────────────────────
//
// Direct gRPC WriteAuditLog → DB verification for bootstrap_completed events.

func TestBootstrap_EmitsAuditEntry(t *testing.T) {
	client := dialCoreGRPCForAudit(t)
	actorID := fmt.Sprintf("audit-bootstrap-%d@example.com", time.Now().UnixNano())
	before := time.Now()

	resp, err := client.WriteAuditLog(context.Background(), &pb.WriteAuditLogRequest{
		ActorUserId: actorID,
		Action:      "bootstrap_completed",
		TargetType:  "server",
		TargetId:    "",
		Outcome:     "success",
	})
	if err != nil {
		t.Fatalf("WriteAuditLog(bootstrap_completed): %v", err)
	}
	if !resp.Ok {
		t.Fatal("WriteAuditLog(bootstrap_completed): core returned ok=false")
	}

	if countAuditLogRows(t, "bootstrap_completed", actorID, before) == 0 {
		t.Error("bootstrap_completed audit entry not found in audit_log after WriteAuditLog gRPC call")
	}
}
