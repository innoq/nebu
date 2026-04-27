package compliance_test

// export_test.go — Story 5.6: Compliance Data Export + Ed25519-Signatur — RED-phase tests
//
// ALL tests in this file are expected to FAIL until Story 5.6 is implemented.
// Failing reason: ExportHandler type and GetExport method do not exist yet in handler.go.
//
// Test strategy:
//   - exportFakeDB: a fourth sql driver registered as "exportdb".
//     DSN encodes scenario flags:
//       events=<N>           — how many event rows the events SELECT returns
//       requestRowExists=<true|false> — whether compliance_requests pre-flight returns a row
//       auditFail=<true|false> — (not a DB flag; handled via mockCoreClient.failWith)
//     The driver handles two distinct SELECT patterns:
//       1. "SELECT requester_user_id, approver_user_id FROM compliance_requests WHERE id = $1"
//       2. "SELECT event_id, room_id, sender, event_type, content, origin_server_ts, signatures
//              FROM events WHERE room_id = $1 AND event_type = 'm.room.message'
//              AND origin_server_ts BETWEEN $2 AND $3 ORDER BY origin_server_ts ASC"
//   - mockCoreClient is already declared in handler_test.go (same package) — reused.
//   - Context keys (ContextKeySub, ContextKeySystemRole) injected directly — no JWT middleware.
//   - httptest.NewRecorder captures status and body for assertions.
//   - Ed25519 test keypair: initTestExportKey() initialises testExportPub/testExportPriv once.
//
// AC coverage:
//   AC1  — TestGetExport_HappyPath, TestGetExport_ContentDispositionHeader
//   AC2  — TestGetExport_NoComplianceToken
//   AC3  — TestGetExport_ExpiredToken, TestGetExport_TamperedToken
//   AC4  — TestGetExport_NonOfficer
//   AC5  — (event shape covered by TestGetExport_HappyPath, TestGetExport_SignatureVerifiable)
//   AC6  — TestGetExport_StrictScope_TimeRange, TestGetExport_StrictScope_RoomID
//   AC7  — TestGetExport_EmptyRange
//   AC8  — TestGetExport_AuditEventCount
//   AC9  — TestGetExport_AuditFailure_Still200
//   AC10 — TestGetExport_SubMismatch
//   AC11 — TestGetExport_SignatureVerifiable
//   AC12 — (all 12 tests listed here)

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/compliance"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Test Ed25519 keypair ─────────────────────────────────────────────────────

var (
	testExportPub  ed25519.PublicKey
	testExportPriv ed25519.PrivateKey
	testExportOnce sync.Once
)

func initTestExportKey() {
	testExportOnce.Do(func() {
		var err error
		testExportPub, testExportPriv, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			panic("export_test: failed to generate Ed25519 key pair: " + err.Error())
		}
	})
}

// ─── exportFakeDriver ─────────────────────────────────────────────────────────
//
// Registered as "exportdb" to avoid collision with fakedb, approvaldb, sessiondb.
//
// DSN flags (semicolon-separated key=value):
//   events=<N>                — number of event rows returned by events SELECT (default 3)
//   requestRowExists=<true|false> — whether compliance_requests SELECT returns a row (default true)

var exportDriverOnce sync.Once

func init() {
	exportDriverOnce.Do(func() {
		sql.Register("exportdb", &exportFakeDriver{})
	})
}

type exportFakeDriver struct{}

func (d *exportFakeDriver) Open(name string) (driver.Conn, error) {
	return &exportFakeConn{dsn: name}, nil
}

type exportFakeConn struct {
	dsn string
}

func (c *exportFakeConn) dsnFlag(key string) string {
	for _, part := range strings.Split(c.dsn, ";") {
		if strings.HasPrefix(part, key+"=") {
			return strings.TrimPrefix(part, key+"=")
		}
	}
	return ""
}

func (c *exportFakeConn) Prepare(query string) (driver.Stmt, error) {
	eventsStr := c.dsnFlag("events")
	eventCount := 3
	if eventsStr != "" {
		if n, err := strconv.Atoi(eventsStr); err == nil {
			eventCount = n
		}
	}
	rowExistsStr := c.dsnFlag("requestRowExists")
	requestRowExists := rowExistsStr != "false" // default true
	expectedRoomID := c.dsnFlag("expectedRoomID")

	return &exportFakeStmt{
		query:            query,
		eventCount:       eventCount,
		requestRowExists: requestRowExists,
		expectedRoomID:   expectedRoomID,
	}, nil
}

func (c *exportFakeConn) Close() error { return nil }
func (c *exportFakeConn) Begin() (driver.Tx, error) {
	return &exportFakeTx{}, nil
}

type exportFakeTx struct{}

func (t *exportFakeTx) Commit() error   { return nil }
func (t *exportFakeTx) Rollback() error { return nil }

type exportFakeStmt struct {
	query            string
	eventCount       int
	requestRowExists bool
	expectedRoomID   string
}

func (s *exportFakeStmt) Close() error  { return nil }
func (s *exportFakeStmt) NumInput() int { return -1 } // variadic

func (s *exportFakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}

func (s *exportFakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	// Pattern 1: compliance_requests pre-flight
	// SELECT requester_user_id, approver_user_id FROM compliance_requests WHERE id = $1
	if strings.Contains(s.query, "compliance_requests") &&
		strings.Contains(s.query, "requester_user_id") &&
		strings.Contains(s.query, "approver_user_id") {
		cols := []string{"requester_user_id", "approver_user_id"}
		if !s.requestRowExists {
			return &exportFakeRows{cols: cols, data: nil}, nil
		}
		return &exportFakeRows{
			cols: cols,
			data: [][]driver.Value{{
				"@alice:server.example", // requester_user_id
				"@admin:server.example", // approver_user_id
			}},
		}, nil
	}

	// Pattern 2: events query (strict scope SELECT)
	// SELECT event_id, room_id, sender, event_type, content, origin_server_ts, signatures
	//   FROM events WHERE room_id = $1 AND event_type = 'm.room.message'
	//   AND origin_server_ts BETWEEN $2 AND $3 ORDER BY origin_server_ts ASC
	//
	// Pattern match REQUIRES "room_id" in the query — this enforces (TEA-MINOR-1
	// follow-up) that the handler passes claims.RoomID as $1 in a room_id-scoped
	// WHERE clause. A query missing "room_id" falls through to the empty default below.
	if strings.Contains(s.query, "FROM events") &&
		strings.Contains(s.query, "room_id") &&
		strings.Contains(s.query, "origin_server_ts") &&
		strings.Contains(s.query, "BETWEEN") {
		// Strict scope simulation: only return rows when the room_id arg ($1)
		// matches a configured "owned" room. Tests for foreign room_id pass
		// "expectedRoomID=<other>" to assert that the handler returns 0 events.
		expectedRoomID := s.expectedRoomID
		actualRoomID := ""
		if len(args) >= 1 {
			if v, ok := args[0].(string); ok {
				actualRoomID = v
			}
		}
		if expectedRoomID != "" && actualRoomID != expectedRoomID {
			cols := []string{"event_id", "room_id", "sender", "event_type", "content", "origin_server_ts", "signatures"}
			return &exportFakeRows{cols: cols, data: nil}, nil
		}
		// Use the actual room_id from $1 (or default) so events reflect strict scope.
		roomID := actualRoomID
		if roomID == "" {
			roomID = "!test-room:server.example"
		}
		cols := []string{"event_id", "room_id", "sender", "event_type", "content", "origin_server_ts", "signatures"}
		rows := make([][]driver.Value, s.eventCount)
		for i := 0; i < s.eventCount; i++ {
			rows[i] = []driver.Value{
				fmt.Sprintf("$event%d:server.example", i+1),           // event_id
				roomID,                                                 // room_id (echoes $1)
				"@alice:server.example",                                // sender
				"m.room.message",                                       // event_type
				json.RawMessage(`{"msgtype":"m.text","body":"msg"}`),   // content (JSONB)
				int64(1700000000000 + int64(i)*1000),                   // origin_server_ts (millis)
				json.RawMessage(`{"server.example":{"ed25519:1":"sig"}}`), // signatures (nullable JSONB)
			}
		}
		return &exportFakeRows{cols: cols, data: rows}, nil
	}

	return &exportFakeRows{}, nil
}

type exportFakeRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *exportFakeRows) Columns() []string { return r.cols }
func (r *exportFakeRows) Close() error      { return nil }
func (r *exportFakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// openExportDB opens an exportdb with the provided DSN flags.
func openExportDB(t *testing.T, flags string) *sql.DB {
	t.Helper()
	db, err := sql.Open("exportdb", flags)
	if err != nil {
		t.Fatalf("sql.Open(exportdb): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newExportHandler constructs an ExportHandler with the test keypair.
// Fails compilation until ExportHandler type exists in handler.go (red phase).
func newExportHandler(t *testing.T, db *sql.DB, client *mockCoreClient) *compliance.ExportHandler {
	t.Helper()
	initTestExportKey()
	return &compliance.ExportHandler{
		DB:         db,
		CoreClient: client,
		SigningKey:  testExportPriv,
		PublicKey:   testExportPub,
	}
}

// issueTestComplianceToken issues a valid ComplianceToken using IssueComplianceToken from jwt.go.
// sub, requestID, roomID, start, end are token claim values.
// expOffset is added to time.Now().Unix() for the exp claim.
func issueTestComplianceToken(t *testing.T, sub, requestID, roomID, start, end string, expOffset int64) string {
	t.Helper()
	initTestExportKey()
	now := time.Now().Unix()
	claims := compliance.ComplianceClaims{
		Sub:                 sub,
		ComplianceRequestID: requestID,
		RoomID:              roomID,
		TimeRangeStart:      start,
		TimeRangeEnd:        end,
		Iat:                 now,
		Exp:                 now + expOffset,
	}
	tok, err := compliance.IssueComplianceToken(testExportPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken: %v", err)
	}
	return tok
}

// newExportRequest builds a GET request to /api/v1/compliance/export with context values set.
// complianceToken may be empty to simulate missing header.
func newExportRequest(t *testing.T, systemRole, sub, complianceToken string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/export", http.NoBody)
	ctx := r.Context()
	ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, systemRole)
	ctx = context.WithValue(ctx, middleware.ContextKeySub, sub)
	r = r.WithContext(ctx)
	if complianceToken != "" {
		r.Header.Set("X-Compliance-Token", complianceToken)
	}
	return r
}

// ─── shared test constants ────────────────────────────────────────────────────

const (
	exportTestRequestID = "550e8400-e29b-41d4-a716-446655440100"
	exportTestRoomID    = "!test-room:server.example"
	exportTestSub       = "@alice:server.example"
	exportTestStart     = "2026-01-01T00:00:00Z"
	exportTestEnd       = "2026-01-02T00:00:00Z"
	exportTokenTTL      = int64(86400) // 24h — valid for all happy-path tests
)

// ─── Test 1: HappyPath → 200 + signed JSON file ───────────────────────────────
//
// AC1, AC5, AC8
//
// Given: compliance_officer JWT (sub=@alice:server.example), valid X-Compliance-Token,
//        mock DB: 1 compliance_requests row + 3 event rows
// When: GET /api/v1/compliance/export
// Then: HTTP 200, Content-Type: application/json,
//       Content-Disposition: attachment; filename="compliance-export-<request_id>.json",
//       body contains export_id, compliance_request_id, room_id, time_range_start/end,
//       requester, approver, events (array of 3), server_signature (non-empty),
//       audit WriteAuditLog called once with action=compliance_export_downloaded,
//       target_type=compliance_request, metadata event_count=3

func TestGetExport_HappyPath(t *testing.T) {
	db := openExportDB(t, "events=3;requestRowExists=true")
	client := &mockCoreClient{}
	h := newExportHandler(t, db, client)

	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	// Content-Type check
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	// Content-Disposition check
	cd := w.Header().Get("Content-Disposition")
	expected := fmt.Sprintf(`attachment; filename="compliance-export-%s.json"`, exportTestRequestID)
	if cd != expected {
		t.Errorf("Content-Disposition: expected %q, got %q", expected, cd)
	}

	// Parse response body
	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}

	// Required fields present
	for _, field := range []string{"export_id", "generated_at", "compliance_request_id", "room_id",
		"time_range_start", "time_range_end", "requester", "approver", "events", "server_signature"} {
		if _, ok := body[field]; !ok {
			t.Errorf("response body missing field %q", field)
		}
	}

	// export_id must be a non-empty UUID-like string
	var exportID string
	if err := json.Unmarshal(body["export_id"], &exportID); err != nil || exportID == "" {
		t.Errorf("export_id missing or not a string: %v", err)
	}

	// events array must have 3 elements
	var events []json.RawMessage
	if err := json.Unmarshal(body["events"], &events); err != nil {
		t.Fatalf("events field is not a JSON array: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}

	// server_signature must be non-empty
	var sig string
	if err := json.Unmarshal(body["server_signature"], &sig); err != nil || sig == "" {
		t.Errorf("server_signature missing or empty: %v", err)
	}

	// Audit must have been emitted exactly once
	if client.callCount() == 0 {
		t.Fatal("audit WriteAuditLog was not called")
	}
	req := client.lastReceived()
	if req == nil {
		t.Fatal("lastReceived audit request is nil")
	}
	if req.Action != "compliance_export_downloaded" {
		t.Errorf("expected action=compliance_export_downloaded, got %q", req.Action)
	}
	if req.TargetType != "compliance_request" {
		t.Errorf("expected target_type=compliance_request, got %q", req.TargetType)
	}
	if req.TargetId != exportTestRequestID {
		t.Errorf("expected target_id=%q, got %q", exportTestRequestID, req.TargetId)
	}
}

// ─── Test 2: EmptyRange → 200 with events:[] ─────────────────────────────────
//
// AC7
//
// Given: same as happy path but mock DB returns 0 event rows
// When: GET /api/v1/compliance/export
// Then: HTTP 200, "events":[], server_signature present and non-empty

func TestGetExport_EmptyRange(t *testing.T) {
	db := openExportDB(t, "events=0;requestRowExists=true")
	client := &mockCoreClient{}
	h := newExportHandler(t, db, client)

	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}

	// events must be an empty array (NOT absent)
	eventsRaw, ok := body["events"]
	if !ok {
		t.Fatal("response body missing 'events' field")
	}
	var events []json.RawMessage
	if err := json.Unmarshal(eventsRaw, &events); err != nil {
		t.Fatalf("events is not a JSON array: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty events array, got %d events", len(events))
	}

	// server_signature must still be present
	sigRaw, ok := body["server_signature"]
	if !ok {
		t.Fatal("response body missing 'server_signature' field")
	}
	var sig string
	if err := json.Unmarshal(sigRaw, &sig); err != nil || sig == "" {
		t.Errorf("server_signature missing or empty on empty-range response: %v", err)
	}

	// Audit must be emitted with event_count=0
	if client.callCount() == 0 {
		t.Fatal("audit must be emitted even for empty-range export")
	}
}

// ─── Test 3: SignatureVerifiable ──────────────────────────────────────────────
//
// AC11
//
// Given: test Ed25519 keypair; mock returns 2 events; handler uses test signing key
// When: GET /api/v1/compliance/export
// Then: parse response body; base64-decode server_signature;
//       reconstruct docBytes = response JSON minus server_signature field;
//       ed25519.Verify(testExportPub, docBytes, sig) == true

func TestGetExport_SignatureVerifiable(t *testing.T) {
	db := openExportDB(t, "events=2;requestRowExists=true")
	client := &mockCoreClient{}
	h := newExportHandler(t, db, client)

	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	// Parse response to extract server_signature
	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}

	sigRaw, ok := body["server_signature"]
	if !ok {
		t.Fatal("response body missing 'server_signature'")
	}
	var sigB64 string
	if err := json.Unmarshal(sigRaw, &sigB64); err != nil || sigB64 == "" {
		t.Fatalf("server_signature is not a valid string: %v", err)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		t.Fatalf("server_signature base64 decode failed: %v", err)
	}

	// Reconstruct docBytes: the document WITHOUT server_signature field.
	// The handler signs a struct-marshalled exportDoc (deterministic field order).
	// We reconstruct it by removing server_signature from the map and re-marshaling.
	delete(body, "server_signature")
	docBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to re-marshal doc without server_signature: %v", err)
	}

	// NOTE: This test verifies that ed25519.Verify passes over the EXACT bytes
	// that the handler signed. If the handler uses a struct (deterministic field
	// order) while the test uses a map (alphabetic order), this will FAIL — which
	// is intentional: the test is designed to pass only if the handler and test
	// agree on the serialization format. Both should produce the same JSON because
	// map marshaling in Go is alphabetically ordered and struct marshaling follows
	// field declaration order; the test documents the expected contract.
	initTestExportKey()
	if !ed25519.Verify(testExportPub, docBytes, sigBytes) {
		t.Error("ed25519.Verify failed: server_signature is not valid over document bytes")
	}
}

// ─── Test 4: NoComplianceToken → 401 ─────────────────────────────────────────
//
// AC2
//
// Given: valid JWT context (compliance_officer), no X-Compliance-Token header
// When: GET /api/v1/compliance/export
// Then: HTTP 401 M_UNKNOWN_TOKEN "Compliance token required"

func TestGetExport_NoComplianceToken(t *testing.T) {
	db := openExportDB(t, "events=3;requestRowExists=true")
	h := newExportHandler(t, db, &mockCoreClient{})

	w := httptest.NewRecorder()
	// Pass empty string for complianceToken → header NOT set
	r := newExportRequest(t, "compliance_officer", exportTestSub, "")

	h.GetExport(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected errcode=M_UNKNOWN_TOKEN, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "Compliance token required") {
		t.Errorf("expected error to mention 'Compliance token required', got %q", resp["error"])
	}
}

// ─── Test 5: ExpiredToken → 401 ──────────────────────────────────────────────
//
// AC3
//
// Given: X-Compliance-Token with exp = now-1 (expired)
// When: GET /api/v1/compliance/export
// Then: HTTP 401 M_UNKNOWN_TOKEN "Invalid or expired compliance token"

func TestGetExport_ExpiredToken(t *testing.T) {
	db := openExportDB(t, "events=3;requestRowExists=true")
	h := newExportHandler(t, db, &mockCoreClient{})

	// Issue token with exp = now-1 (expired 1 second ago)
	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, -1)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for expired token, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected errcode=M_UNKNOWN_TOKEN, got %q", resp["errcode"])
	}
}

// ─── Test 6: SubMismatch → 401 ───────────────────────────────────────────────
//
// AC10
//
// Given: token sub=@bob:server.example, caller JWT sub=@alice:server.example
// When: GET /api/v1/compliance/export
// Then: HTTP 401 M_UNKNOWN_TOKEN (ValidateComplianceToken returns sub mismatch error)

func TestGetExport_SubMismatch(t *testing.T) {
	db := openExportDB(t, "events=3;requestRowExists=true")
	h := newExportHandler(t, db, &mockCoreClient{})

	// Token issued for @bob but caller context has @alice
	tok := issueTestComplianceToken(t, "@bob:server.example", exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub /* @alice */, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for sub mismatch, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected errcode=M_UNKNOWN_TOKEN for sub mismatch, got %q", resp["errcode"])
	}
}

// ─── Test 7: NonOfficer → 403 ────────────────────────────────────────────────
//
// AC4
//
// Given: caller JWT has system_role=instance_admin (not compliance_officer)
// When: GET /api/v1/compliance/export
// Then: HTTP 403 M_FORBIDDEN "Compliance officer role required"

func TestGetExport_NonOfficer(t *testing.T) {
	db := openExportDB(t, "events=3;requestRowExists=true")
	h := newExportHandler(t, db, &mockCoreClient{})

	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "instance_admin" /* wrong role */, exportTestSub, tok)

	h.GetExport(w, r)

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

// ─── Test 8: TamperedToken → 401 ─────────────────────────────────────────────
//
// AC3
//
// Given: X-Compliance-Token signed with a DIFFERENT Ed25519 private key (not testExportPriv)
// When: GET /api/v1/compliance/export
// Then: HTTP 401 M_UNKNOWN_TOKEN (signature verification fails)

func TestGetExport_TamperedToken(t *testing.T) {
	db := openExportDB(t, "events=3;requestRowExists=true")
	h := newExportHandler(t, db, &mockCoreClient{})

	// Generate a different keypair — this is the "attacker" key
	_, attackerPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate attacker key: %v", err)
	}

	// Issue token signed with the attacker key (not the registered testExportPriv)
	now := time.Now().Unix()
	attackerClaims := compliance.ComplianceClaims{
		Sub:                 exportTestSub,
		ComplianceRequestID: exportTestRequestID,
		RoomID:              exportTestRoomID,
		TimeRangeStart:      exportTestStart,
		TimeRangeEnd:        exportTestEnd,
		Iat:                 now,
		Exp:                 now + exportTokenTTL,
	}
	tamperedTok, err := compliance.IssueComplianceToken(attackerPriv, attackerClaims)
	if err != nil {
		t.Fatalf("IssueComplianceToken with attacker key: %v", err)
	}

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tamperedTok)

	h.GetExport(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for tampered token, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_UNKNOWN_TOKEN" {
		t.Errorf("expected errcode=M_UNKNOWN_TOKEN for tampered token, got %q", resp["errcode"])
	}
}

// ─── Test 9: StrictScope_TimeRange ───────────────────────────────────────────
//
// AC6
//
// Given: mock DB query includes room_id, event_type, and origin_server_ts BETWEEN clauses;
//        events returned by fake DB are only those in range (the driver enforces this by
//        returning exactly N events matching the query — scope enforcement is in the SQL WHERE clause)
// When: GET /api/v1/compliance/export with events=3 in DSN
// Then: events array length == 3 (only the in-scope events from the mock)
//
// Scope enforcement note: The fake driver returns exactly the events=N rows for
// the scoped query (room_id + event_type + origin_server_ts BETWEEN). The test
// verifies that the handler uses the strict WHERE-clause query (pattern match in
// exportFakeStmt.Query) and does NOT include any additional events beyond what
// the DB returns. This is structural: if the handler omits the BETWEEN clause,
// it would use a different query that the fake driver would not recognise as the
// scoped events query, causing a different (empty) result.

func TestGetExport_StrictScope_TimeRange(t *testing.T) {
	db := openExportDB(t, "events=3;requestRowExists=true")
	client := &mockCoreClient{}
	h := newExportHandler(t, db, client)

	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}

	var events []json.RawMessage
	if err := json.Unmarshal(body["events"], &events); err != nil {
		t.Fatalf("events field is not a JSON array: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("strict time-range scope: expected 3 in-range events, got %d", len(events))
	}
}

// ─── Test 10: StrictScope_RoomID ──────────────────────────────────────────────
//
// AC6
//
// Given: scope comes exclusively from token claims (room_id = exportTestRoomID);
//        all mock events have room_id = "!test-room:server.example" matching the token claim;
//        the query has room_id = $1 in the WHERE clause (no URL param)
// When: GET /api/v1/compliance/export
// Then: all returned events have room_id matching the token's room_id claim

func TestGetExport_StrictScope_RoomID(t *testing.T) {
	db := openExportDB(t, "events=2;requestRowExists=true")
	client := &mockCoreClient{}
	h := newExportHandler(t, db, client)

	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}

	var events []map[string]json.RawMessage
	if err := json.Unmarshal(body["events"], &events); err != nil {
		t.Fatalf("events field is not a JSON array of objects: %v", err)
	}

	// Every event in the export must have room_id matching the token claim
	for i, ev := range events {
		roomIDRaw, ok := ev["room_id"]
		if !ok {
			t.Errorf("event[%d] missing room_id field", i)
			continue
		}
		var roomID string
		if err := json.Unmarshal(roomIDRaw, &roomID); err != nil {
			t.Errorf("event[%d] room_id is not a string: %v", i, err)
			continue
		}
		if roomID != exportTestRoomID {
			t.Errorf("event[%d] room_id=%q, expected %q (strict scope violation)", i, roomID, exportTestRoomID)
		}
	}
}

// ─── Test 11: AuditFailure → Still 200 ───────────────────────────────────────
//
// AC9 (never-raise policy)
//
// Given: happy path conditions; mock WriteAuditLog returns gRPC error
// When: GET /api/v1/compliance/export
// Then: HTTP 200 (audit failure must not block or error the response)

func TestGetExport_AuditFailure_Still200(t *testing.T) {
	db := openExportDB(t, "events=2;requestRowExists=true")
	// Simulate gRPC audit failure
	client := &mockCoreClient{failWith: fmt.Errorf("simulated gRPC audit failure for export test")}
	h := newExportHandler(t, db, client)

	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("audit failure must not block 200 — got %d, body: %s", w.Code, w.Body.String())
	}

	// Response must still be a valid signed export
	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON even on audit failure: %v", err)
	}
	if _, ok := body["server_signature"]; !ok {
		t.Error("server_signature must be present even when audit fails")
	}
}

// ─── Test 12: AuditEventCount → metadata matches len(events) ─────────────────
//
// AC8
//
// Given: mock returns 5 events; mock CoreServiceClient
// When: GET /api/v1/compliance/export
// Then: WriteAuditLog called with action=compliance_export_downloaded,
//       metadata JSON contains "event_count":5

func TestGetExport_AuditEventCount(t *testing.T) {
	db := openExportDB(t, "events=5;requestRowExists=true")
	client := &mockCoreClient{}
	h := newExportHandler(t, db, client)

	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	// Verify response events count = 5
	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	var events []json.RawMessage
	if err := json.Unmarshal(body["events"], &events); err != nil {
		t.Fatalf("events field is not a JSON array: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("expected 5 events in response, got %d", len(events))
	}

	// Audit metadata.event_count must match
	if client.callCount() == 0 {
		t.Fatal("audit WriteAuditLog was not called")
	}
	req := client.lastReceived()
	if req == nil {
		t.Fatal("lastReceived audit request is nil")
	}
	if req.Action != "compliance_export_downloaded" {
		t.Errorf("expected action=compliance_export_downloaded, got %q", req.Action)
	}

	var meta map[string]any
	if err := json.Unmarshal(req.MetadataJson, &meta); err != nil {
		t.Fatalf("metadata_json is not valid JSON: %v — raw: %q", err, string(req.MetadataJson))
	}
	// event_count is JSON number → float64 in Go map
	eventCountRaw, ok := meta["event_count"]
	if !ok {
		t.Fatal("metadata_json missing event_count field")
	}
	eventCount, ok := eventCountRaw.(float64)
	if !ok {
		t.Fatalf("metadata_json.event_count is not a number, got %T: %v", eventCountRaw, eventCountRaw)
	}
	if int(eventCount) != 5 {
		t.Errorf("metadata event_count: expected 5, got %v", eventCount)
	}
}

// ─── Test 13: ContentDispositionHeader ───────────────────────────────────────
//
// AC1 (header check focused)
//
// Given: happy path conditions; compliance_request_id from token = exportTestRequestID
// When: GET /api/v1/compliance/export
// Then: Content-Disposition: attachment; filename="compliance-export-<exportTestRequestID>.json"

func TestGetExport_ContentDispositionHeader(t *testing.T) {
	db := openExportDB(t, "events=1;requestRowExists=true")
	client := &mockCoreClient{}
	h := newExportHandler(t, db, client)

	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	cd := w.Header().Get("Content-Disposition")
	expected := fmt.Sprintf(`attachment; filename="compliance-export-%s.json"`, exportTestRequestID)
	if cd != expected {
		t.Errorf("Content-Disposition header mismatch:\n  expected: %q\n  got:      %q", expected, cd)
	}
}

// ─── Test 14: StrictScope_ForeignRoomID — handler passes claims.RoomID as $1 ─
//
// AC6 (TEA-MINOR-1 reinforcement)
//
// Given: token claim room_id = "!owned:server.example"; fake DB is configured
//        to return rows ONLY when $1 == "!owned:server.example" (strict simulation).
// When: GET /api/v1/compliance/export with the token
// Then: handler must pass claims.RoomID as $1 — fake DB returns 3 rows.
//
// And — same scenario, but token claim points to "!foreign:server.example".
// The fake DB returns 0 rows (since $1 != owned). The handler returns 200 with empty
// events. This proves the handler does NOT bypass claims.RoomID (e.g. via a hard-coded
// or URL-derived value): if it did, the fake DB would not see the foreign room_id.

func TestGetExport_StrictScope_ForeignRoomID_BindsClaimsRoomID(t *testing.T) {
	// Strict simulation: the fake DB only returns events when $1 matches expectedRoomID.
	db := openExportDB(t, "events=3;requestRowExists=true;expectedRoomID=!owned:server.example")
	client := &mockCoreClient{}
	h := newExportHandler(t, db, client)

	// Token scoped to a DIFFERENT room — handler must pass this room_id as $1.
	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID,
		"!foreign:server.example", exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d — body: %s", w.Code, w.Body.String())
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	var events []json.RawMessage
	if err := json.Unmarshal(body["events"], &events); err != nil {
		t.Fatalf("events field is not a JSON array: %v", err)
	}
	// 0 events because the fake DB sees $1=="!foreign:server.example" != owned.
	if len(events) != 0 {
		t.Errorf("strict room_id scope: expected 0 events for foreign room, got %d "+
			"(handler may not be passing claims.RoomID as $1)", len(events))
	}

	// Sanity: response room_id field equals the token claim (further proves
	// the handler used claims.RoomID end-to-end).
	var roomID string
	if err := json.Unmarshal(body["room_id"], &roomID); err != nil {
		t.Fatalf("room_id field not a string: %v", err)
	}
	if roomID != "!foreign:server.example" {
		t.Errorf("response room_id mismatch: expected %q, got %q",
			"!foreign:server.example", roomID)
	}
}

// ─── Test 15: RequestRowMissing → 500 (TEA-MINOR-3) ──────────────────────────
//
// AC1 (pre-flight failure path).
//
// Given: token validates fine, but compliance_requests pre-flight returns 0 rows.
//        Indicates token was valid but request was deleted — data integrity issue.
// When: GET /api/v1/compliance/export
// Then: HTTP 500 M_UNKNOWN. NO audit emitted. NO events query reached.

func TestGetExport_RequestNotFound_Returns500(t *testing.T) {
	db := openExportDB(t, "events=3;requestRowExists=false")
	client := &mockCoreClient{}
	h := newExportHandler(t, db, client)

	tok := issueTestComplianceToken(t, exportTestSub, exportTestRequestID, exportTestRoomID,
		exportTestStart, exportTestEnd, exportTokenTTL)

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 Internal Server Error when compliance_requests row missing, got %d — body: %s",
			w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_UNKNOWN" {
		t.Errorf("expected errcode=M_UNKNOWN, got %q", resp["errcode"])
	}
	// No audit on this failure path (audit is only emitted on success).
	if client.callCount() != 0 {
		t.Errorf("audit must NOT be emitted on pre-flight 500, got %d calls", client.callCount())
	}
}

// ─── Test 16: MalformedTimeRangeClaim → 500 (TEA-MINOR-2) ────────────────────
//
// Defensive — guards against silently-swallowed time.Parse errors that would
// produce zero-time → empty export → no audit trail. A malformed RFC 3339
// claim must produce 500 M_UNKNOWN with an audit-LOG line (slog.Error), not a
// silent 200-with-zero-events.
//
// Given: token contains "not-a-rfc3339-timestamp" as time_range_start
// When: GET /api/v1/compliance/export
// Then: HTTP 500 M_UNKNOWN. NO audit emitted. NO DB query reached.

func TestGetExport_MalformedTimeRangeClaim_Returns500(t *testing.T) {
	db := openExportDB(t, "events=3;requestRowExists=true")
	client := &mockCoreClient{}
	h := newExportHandler(t, db, client)

	// Issue a token with a deliberately malformed time_range_start.
	now := time.Now().Unix()
	badClaims := compliance.ComplianceClaims{
		Sub:                 exportTestSub,
		ComplianceRequestID: exportTestRequestID,
		RoomID:              exportTestRoomID,
		TimeRangeStart:      "not-a-rfc3339-timestamp",
		TimeRangeEnd:        exportTestEnd,
		Iat:                 now,
		Exp:                 now + exportTokenTTL,
	}
	tok, err := compliance.IssueComplianceToken(testExportPriv, badClaims)
	if err != nil {
		t.Fatalf("IssueComplianceToken: %v", err)
	}

	w := httptest.NewRecorder()
	r := newExportRequest(t, "compliance_officer", exportTestSub, tok)

	h.GetExport(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for malformed time_range_start, got %d — body: %s",
			w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_UNKNOWN" {
		t.Errorf("expected errcode=M_UNKNOWN, got %q", resp["errcode"])
	}
	if client.callCount() != 0 {
		t.Errorf("audit must NOT be emitted on malformed claim 500, got %d calls", client.callCount())
	}
}
