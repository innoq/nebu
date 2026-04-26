package compliance_test

// handler_test.go — Story 5.3: Compliance Access Request API — RED-phase tests
//
// ALL tests in this file are expected to FAIL until Story 5.3 is implemented.
// Failing reason: PostAccessRequest returns 501 Not Implemented (stub).
//
// Test strategy:
//   - mockDB implements the DB interface via a minimal sql.DB wrapper driven by
//     controlled return values (no real PostgreSQL required).
//   - mockCoreClient implements pb.CoreServiceClient with a WriteAuditLog stub
//     that records the call; all other methods are no-ops.
//   - Context keys (ContextKeySub, ContextKeySystemRole) are injected directly
//     into the request context — no JWT middleware is involved in unit tests.
//   - httptest.NewRecorder captures status and body for assertions.
//
// Tests:
//  1. TestPostAccessRequest_ValidPayload_Returns201         — AC1,AC3,AC4,AC5,AC6
//  2. TestPostAccessRequest_NonComplianceOfficer_Returns403 — AC1
//  3. TestPostAccessRequest_UnknownRoom_Returns404          — AC3
//  4. TestPostAccessRequest_MissingJustification_Returns400 — AC2.9
//  5. TestPostAccessRequest_JustificationTooShort_Returns400— AC2.10
//  6. TestPostAccessRequest_InvalidTimeRange_Returns400     — AC2.8
//  7. TestPostAccessRequest_InvalidContentType_Returns415   — AC2.1
//  8. TestPostAccessRequest_UnknownField_Returns400         — AC2.2
//  9. TestPostAccessRequest_AuditEmittedOnSuccess           — AC5

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/compliance"
	"github.com/nebu/nebu/internal/middleware"
	"google.golang.org/grpc"
)

// uuidRegex matches canonical 8-4-4-4-12 lowercase hex UUID strings.
// Used by TestPostAccessRequest_ValidPayload_Returns201 (MINOR-2) to assert
// that request_id is a well-formed UUID, not merely a non-empty string.
var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ─── mockCoreClient ──────────────────────────────────────────────────────────
//
// Records WriteAuditLog calls; all other methods are no-ops.

type mockCoreClient struct {
	mu       sync.Mutex
	received []*pb.WriteAuditLogRequest
	// failWith, when non-nil, is returned by WriteAuditLog instead of a successful
	// response. Used by TestPostAccessRequest_AuditFailure_Still201 to verify the
	// never-raise policy (MINOR-5).
	failWith error
}

func (m *mockCoreClient) WriteAuditLog(_ context.Context, req *pb.WriteAuditLogRequest, _ ...grpc.CallOption) (*pb.WriteAuditLogResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.received = append(m.received, req)
	if m.failWith != nil {
		return nil, m.failWith
	}
	return &pb.WriteAuditLogResponse{Ok: true}, nil
}

func (m *mockCoreClient) lastReceived() *pb.WriteAuditLogRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.received) == 0 {
		return nil
	}
	return m.received[len(m.received)-1]
}

func (m *mockCoreClient) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.received)
}

// No-op stubs for all other CoreServiceClient methods.

func (m *mockCoreClient) SendEvent(_ context.Context, _ *pb.SendEventRequest, _ ...grpc.CallOption) (*pb.SendEventResponse, error) {
	return &pb.SendEventResponse{}, nil
}
func (m *mockCoreClient) CreateRoom(_ context.Context, _ *pb.CreateRoomRequest, _ ...grpc.CallOption) (*pb.CreateRoomResponse, error) {
	return &pb.CreateRoomResponse{}, nil
}
func (m *mockCoreClient) JoinRoom(_ context.Context, _ *pb.JoinRoomRequest, _ ...grpc.CallOption) (*pb.JoinRoomResponse, error) {
	return &pb.JoinRoomResponse{}, nil
}
func (m *mockCoreClient) LeaveRoom(_ context.Context, _ *pb.LeaveRoomRequest, _ ...grpc.CallOption) (*pb.LeaveRoomResponse, error) {
	return &pb.LeaveRoomResponse{}, nil
}
func (m *mockCoreClient) GetMessages(_ context.Context, _ *pb.GetMessagesRequest, _ ...grpc.CallOption) (*pb.GetMessagesResponse, error) {
	return &pb.GetMessagesResponse{}, nil
}
func (m *mockCoreClient) SetPresence(_ context.Context, _ *pb.SetPresenceRequest, _ ...grpc.CallOption) (*pb.SetPresenceResponse, error) {
	return &pb.SetPresenceResponse{}, nil
}
func (m *mockCoreClient) SetTyping(_ context.Context, _ *pb.SetTypingRequest, _ ...grpc.CallOption) (*pb.SetTypingResponse, error) {
	return &pb.SetTypingResponse{}, nil
}
func (m *mockCoreClient) ValidateToken(_ context.Context, _ *pb.ValidateTokenRequest, _ ...grpc.CallOption) (*pb.ValidateTokenResponse, error) {
	return &pb.ValidateTokenResponse{}, nil
}
func (m *mockCoreClient) GetPendingEvents(_ context.Context, _ *pb.GetPendingEventsRequest, _ ...grpc.CallOption) (*pb.GetPendingEventsResponse, error) {
	return &pb.GetPendingEventsResponse{}, nil
}
func (m *mockCoreClient) EventBus(_ context.Context, _ *pb.EventBusRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[pb.Event], error) {
	return nil, nil
}
func (m *mockCoreClient) GetMetrics(_ context.Context, _ *pb.GetMetricsRequest, _ ...grpc.CallOption) (*pb.GetMetricsResponse, error) {
	return &pb.GetMetricsResponse{}, nil
}
func (m *mockCoreClient) GetRoomState(_ context.Context, _ *pb.GetRoomStateRequest, _ ...grpc.CallOption) (*pb.GetRoomStateResponse, error) {
	return &pb.GetRoomStateResponse{}, nil
}
func (m *mockCoreClient) InviteUser(_ context.Context, _ *pb.InviteUserRequest, _ ...grpc.CallOption) (*pb.InviteUserResponse, error) {
	return &pb.InviteUserResponse{}, nil
}
func (m *mockCoreClient) SetPowerLevels(_ context.Context, _ *pb.SetPowerLevelsRequest, _ ...grpc.CallOption) (*pb.SetPowerLevelsResponse, error) {
	return &pb.SetPowerLevelsResponse{}, nil
}
func (m *mockCoreClient) SendReceipt(_ context.Context, _ *pb.SendReceiptRequest, _ ...grpc.CallOption) (*pb.SendReceiptResponse, error) {
	return &pb.SendReceiptResponse{}, nil
}
func (m *mockCoreClient) GetInitialSync(_ context.Context, _ *pb.GetInitialSyncRequest, _ ...grpc.CallOption) (*pb.GetInitialSyncResponse, error) {
	return &pb.GetInitialSyncResponse{}, nil
}
func (m *mockCoreClient) GetSyncDelta(_ context.Context, _ *pb.GetSyncDeltaRequest, _ ...grpc.CallOption) (*pb.GetSyncDeltaResponse, error) {
	return &pb.GetSyncDeltaResponse{}, nil
}
func (m *mockCoreClient) GetPresence(_ context.Context, _ *pb.GetPresenceRequest, _ ...grpc.CallOption) (*pb.GetPresenceResponse, error) {
	return &pb.GetPresenceResponse{}, nil
}
func (m *mockCoreClient) UpdateProfile(_ context.Context, _ *pb.UpdateProfileRequest, _ ...grpc.CallOption) (*pb.UpdateProfileResponse, error) {
	return &pb.UpdateProfileResponse{}, nil
}

// ─── fakeDB driver ────────────────────────────────────────────────────────────
//
// A minimal database/sql/driver implementation that returns scripted rows for
// SELECT queries (room existence check) and a UUID string for INSERT...RETURNING.
//
// fakeDriver is registered once per test binary under a unique name so that
// parallel tests sharing the same driver registration don't race.

var fakeDriverOnce sync.Once

func init() {
	fakeDriverOnce.Do(func() {
		sql.Register("fakedb", &fakeDriver{})
	})
}

type fakeDriver struct{}

func (d *fakeDriver) Open(name string) (driver.Conn, error) {
	return &fakeConn{dsn: name}, nil
}

// fakeConn holds scripted query behaviour encoded in the DSN string.
// DSN format: "roomFound=<true|false>"
type fakeConn struct {
	dsn string
}

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	roomFound := strings.Contains(c.dsn, "roomFound=true")
	return &fakeStmt{query: query, roomFound: roomFound}, nil
}

func (c *fakeConn) Close() error  { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) {
	return &fakeTx{}, nil
}

type fakeTx struct{}

func (t *fakeTx) Commit() error   { return nil }
func (t *fakeTx) Rollback() error { return nil }

type fakeStmt struct {
	query     string
	roomFound bool
}

func (s *fakeStmt) Close() error { return nil }
func (s *fakeStmt) NumInput() int { return -1 } // variadic

func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}

func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	// SELECT 1 FROM rooms WHERE room_id = $1
	if strings.Contains(s.query, "FROM rooms") {
		if s.roomFound {
			return &fakeRows{cols: []string{"?column?"}, data: [][]driver.Value{{int64(1)}}}, nil
		}
		return &fakeRows{cols: []string{"?column?"}, data: nil}, nil
	}
	// INSERT INTO compliance_requests ... RETURNING id
	if strings.Contains(s.query, "compliance_requests") {
		fakeUUID := "550e8400-e29b-41d4-a716-446655440000"
		return &fakeRows{cols: []string{"id"}, data: [][]driver.Value{{fakeUUID}}}, nil
	}
	return &fakeRows{}, nil
}

type fakeRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		// database/sql distinguishes io.EOF (→ ErrNoRows) from other errors (propagated as-is).
		// We must return io.EOF here so that QueryRowContext.Scan returns sql.ErrNoRows when
		// no rows exist (e.g. room not found), not a generic error.
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func openFakeDB(t *testing.T, roomFound bool) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("roomFound=%v", roomFound)
	db, err := sql.Open("fakedb", dsn)
	if err != nil {
		t.Fatalf("sql.Open(fakedb): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// complianceRequest is the JSON payload accepted by PostAccessRequest.
type complianceRequest struct {
	RoomID         string `json:"room_id"`
	TimeRangeStart string `json:"time_range_start"`
	TimeRangeEnd   string `json:"time_range_end"`
	Justification  string `json:"justification"`
}

func validBody() complianceRequest {
	return complianceRequest{
		RoomID:         "!abc123:server.example",
		TimeRangeStart: "2026-01-01T00:00:00Z",
		TimeRangeEnd:   "2026-03-31T23:59:59Z",
		Justification:  "Investigating policy violation under reference ABC-2026-001",
	}
}

func newRequest(t *testing.T, body any, systemRole, sub, contentType string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("json.Encode: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/access-requests", &buf)
	r.Header.Set("Content-Type", contentType)

	ctx := r.Context()
	ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, systemRole)
	ctx = context.WithValue(ctx, middleware.ContextKeySub, sub)
	return r.WithContext(ctx)
}

func newJSONRequest(t *testing.T, body any, systemRole, sub string) *http.Request {
	return newRequest(t, body, systemRole, sub, "application/json")
}

// ─── Test 1: ValidPayload_Returns201 ──────────────────────────────────────────
//
// AC1, AC3, AC4, AC5, AC6
//
// Given: compliance_officer role in context, valid body, mock DB returning UUID
// When: POST /api/v1/compliance/access-requests
// Then: 201, JSON {"request_id":"<uuid>","status":"pending"}

func TestPostAccessRequest_ValidPayload_Returns201(t *testing.T) {
	db := openFakeDB(t, true /* roomFound */)
	client := &mockCoreClient{}
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: client}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, validBody(), "compliance_officer", "sub-officer-1")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v — body: %s", err, w.Body.String())
	}
	// MINOR-2: assert the returned request_id is a canonical 8-4-4-4-12 UUID,
	// not merely a non-empty string. Catches accidental mis-scan (e.g. returning
	// status or time columns) and enforces the AC6 contract shape.
	if !uuidRegex.MatchString(resp["request_id"]) {
		t.Errorf("expected request_id to be a canonical UUID, got %q", resp["request_id"])
	}
	if resp["status"] != "pending" {
		t.Errorf("expected status=pending, got %q", resp["status"])
	}
}

// ─── Test 2: NonComplianceOfficer_Returns403 ─────────────────────────────────
//
// AC1
//
// Given: system_role = "user" in context
// When: POST /api/v1/compliance/access-requests
// Then: 403 M_FORBIDDEN

func TestPostAccessRequest_NonComplianceOfficer_Returns403(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, validBody(), "user", "sub-regular-user")

	h.PostAccessRequest(w, r)

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
}

// ─── Test 3: UnknownRoom_Returns404 ───────────────────────────────────────────
//
// AC3
//
// Given: compliance_officer role, room_id NOT in mock DB
// When: POST /api/v1/compliance/access-requests
// Then: 404 M_NOT_FOUND

func TestPostAccessRequest_UnknownRoom_Returns404(t *testing.T) {
	db := openFakeDB(t, false /* roomFound=false */)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, validBody(), "compliance_officer", "sub-officer-1")

	h.PostAccessRequest(w, r)

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
}

// ─── Test 4: MissingJustification_Returns400 ─────────────────────────────────
//
// AC2.9
//
// Given: valid role, body without justification field
// When: POST /api/v1/compliance/access-requests
// Then: 400 M_BAD_JSON

func TestPostAccessRequest_MissingJustification_Returns400(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	body := map[string]string{
		"room_id":          "!abc123:server.example",
		"time_range_start": "2026-01-01T00:00:00Z",
		"time_range_end":   "2026-03-31T23:59:59Z",
		// justification intentionally omitted
	}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, body, "compliance_officer", "sub-officer-1")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
}

// ─── Test 5: JustificationTooShort_Returns400 ────────────────────────────────
//
// AC2.10
//
// Given: valid role, justification < 20 characters
// When: POST /api/v1/compliance/access-requests
// Then: 400 M_BAD_JSON "justification must be at least 20 characters"

func TestPostAccessRequest_JustificationTooShort_Returns400(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	body := complianceRequest{
		RoomID:         "!abc123:server.example",
		TimeRangeStart: "2026-01-01T00:00:00Z",
		TimeRangeEnd:   "2026-03-31T23:59:59Z",
		Justification:  "short", // 5 chars — below 20-char minimum
	}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, body, "compliance_officer", "sub-officer-1")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "20 characters") {
		t.Errorf("expected error to mention '20 characters', got %q", resp["error"])
	}
}

// ─── Test 6: InvalidTimeRange_Returns400 ─────────────────────────────────────
//
// AC2.8
//
// Given: valid role, time_range_end <= time_range_start
// When: POST /api/v1/compliance/access-requests
// Then: 400 M_BAD_JSON "time_range_end must be after time_range_start"

func TestPostAccessRequest_InvalidTimeRange_Returns400(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	body := complianceRequest{
		RoomID:         "!abc123:server.example",
		TimeRangeStart: "2026-03-31T23:59:59Z",
		TimeRangeEnd:   "2026-01-01T00:00:00Z", // end is BEFORE start
		Justification:  "Investigating policy violation under reference ABC-2026-001",
	}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, body, "compliance_officer", "sub-officer-1")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "time_range_end must be after time_range_start") {
		t.Errorf("expected specific error message, got %q", resp["error"])
	}
}

// ─── Test 7: InvalidContentType_Returns415 ───────────────────────────────────
//
// AC2.1
//
// Given: compliance_officer role, Content-Type: application/x-www-form-urlencoded
// When: POST /api/v1/compliance/access-requests
// Then: 415 M_UNSUPPORTED_MEDIA_TYPE

func TestPostAccessRequest_InvalidContentType_Returns415(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	w := httptest.NewRecorder()
	r := newRequest(t, validBody(), "compliance_officer", "sub-officer-1", "application/x-www-form-urlencoded")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 Unsupported Media Type, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_UNSUPPORTED_MEDIA_TYPE" {
		t.Errorf("expected errcode=M_UNSUPPORTED_MEDIA_TYPE, got %q", resp["errcode"])
	}
}

// ─── Test 8: UnknownField_Returns400 ─────────────────────────────────────────
//
// AC2.2 (DisallowUnknownFields)
//
// Given: valid role, body with an extra unknown field "sneaky"
// When: POST /api/v1/compliance/access-requests
// Then: 400 M_BAD_JSON

func TestPostAccessRequest_UnknownField_Returns400(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	// Build body manually to include unknown field.
	bodyJSON := `{
		"room_id": "!abc123:server.example",
		"time_range_start": "2026-01-01T00:00:00Z",
		"time_range_end": "2026-03-31T23:59:59Z",
		"justification": "Investigating policy violation under reference ABC-2026-001",
		"sneaky": "injection attempt"
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/access-requests", strings.NewReader(bodyJSON))
	r.Header.Set("Content-Type", "application/json")

	ctx := r.Context()
	ctx = context.WithValue(ctx, middleware.ContextKeySystemRole, "compliance_officer")
	ctx = context.WithValue(ctx, middleware.ContextKeySub, "sub-officer-1")
	r = r.WithContext(ctx)

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for unknown field, got %d — body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
}

// ─── Test 9: AuditEmittedOnSuccess ───────────────────────────────────────────
//
// AC5
//
// Given: valid request, mock DB and mock gRPC client
// When: POST /api/v1/compliance/access-requests succeeds
// Then: mockCoreClient.WriteAuditLog was called with
//
//	action="compliance_access_requested", target_type="room",
//	target_id=room_id, outcome="success"

func TestPostAccessRequest_AuditEmittedOnSuccess(t *testing.T) {
	db := openFakeDB(t, true /* roomFound */)
	client := &mockCoreClient{}
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: client}

	b := validBody()
	w := httptest.NewRecorder()
	r := newJSONRequest(t, b, "compliance_officer", "sub-officer-audit")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("handler must return 201 for audit test to be meaningful, got %d — body: %s", w.Code, w.Body.String())
	}

	if client.callCount() == 0 {
		t.Fatal("audit.LogEvent was NOT called — WriteAuditLog call count is 0")
	}

	req := client.lastReceived()
	if req == nil {
		t.Fatal("lastReceived audit request is nil")
	}
	if req.Action != "compliance_access_requested" {
		t.Errorf("expected action=compliance_access_requested, got %q", req.Action)
	}
	if req.TargetType != "room" {
		t.Errorf("expected target_type=room, got %q", req.TargetType)
	}
	if req.TargetId != b.RoomID {
		t.Errorf("expected target_id=%q, got %q", b.RoomID, req.TargetId)
	}
	if req.Outcome != "success" {
		t.Errorf("expected outcome=success, got %q", req.Outcome)
	}
	if req.ActorUserId != "sub-officer-audit" {
		t.Errorf("expected actor_user_id=sub-officer-audit, got %q", req.ActorUserId)
	}

	// MINOR-1: PII-hygiene — the MetadataJson must carry justification_length
	// (the truthful derived signal) and MUST NOT contain the raw justification
	// text (PII risk — justifications frequently reference user names / case IDs).
	var meta map[string]any
	if err := json.Unmarshal(req.MetadataJson, &meta); err != nil {
		t.Fatalf("metadata_json is not valid JSON: %v — raw=%q", err, string(req.MetadataJson))
	}
	wantLen := float64(len(b.Justification)) // json.Unmarshal decodes numbers as float64
	if got, ok := meta["justification_length"].(float64); !ok || got != wantLen {
		t.Errorf("expected metadata.justification_length=%v, got %v (type %T)", wantLen, meta["justification_length"], meta["justification_length"])
	}
	if strings.Contains(string(req.MetadataJson), b.Justification) {
		t.Errorf("metadata_json must NOT contain the raw justification text (PII leak) — got %q", string(req.MetadataJson))
	}
	if _, leaked := meta["justification"]; leaked {
		t.Errorf("metadata_json must NOT carry a 'justification' key (PII leak) — got %v", meta)
	}
}

// ─── MINOR-4: Missing validation-rule tests (AC2.3 – AC2.7) ──────────────────
//
// These complete the validation coverage that the story's AC2 list specifies
// but the original 9-test suite did not exercise directly:
//
//   AC2.3 — room_id required
//   AC2.4 — room_id must be a valid Matrix room ID
//   AC2.5 — time_range_start required
//   AC2.6 — time_range_end required
//   AC2.7 — timestamps must parse as RFC 3339
//
// Each test uses a map[string]any body so an omitted field is truly absent
// (rather than zero-valued), mirroring real client behaviour.

func TestPostAccessRequest_MissingRoomID_Returns400(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	body := map[string]any{
		// room_id intentionally omitted
		"time_range_start": "2026-01-01T00:00:00Z",
		"time_range_end":   "2026-03-31T23:59:59Z",
		"justification":    "Investigating policy violation under reference ABC-2026-001",
	}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, body, "compliance_officer", "sub-officer-1")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d — body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "room_id") {
		t.Errorf("expected error to reference room_id, got %q", resp["error"])
	}
}

func TestPostAccessRequest_InvalidMatrixRoomID_Returns400(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	body := complianceRequest{
		RoomID:         "not-a-matrix-room-id", // missing leading "!" + ":server"
		TimeRangeStart: "2026-01-01T00:00:00Z",
		TimeRangeEnd:   "2026-03-31T23:59:59Z",
		Justification:  "Investigating policy violation under reference ABC-2026-001",
	}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, body, "compliance_officer", "sub-officer-1")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d — body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
}

func TestPostAccessRequest_MissingTimeRangeStart_Returns400(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	body := map[string]any{
		"room_id": "!abc123:server.example",
		// time_range_start intentionally omitted
		"time_range_end": "2026-03-31T23:59:59Z",
		"justification":  "Investigating policy violation under reference ABC-2026-001",
	}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, body, "compliance_officer", "sub-officer-1")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d — body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "time_range_start") {
		t.Errorf("expected error to reference time_range_start, got %q", resp["error"])
	}
}

func TestPostAccessRequest_MissingTimeRangeEnd_Returns400(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	body := map[string]any{
		"room_id":          "!abc123:server.example",
		"time_range_start": "2026-01-01T00:00:00Z",
		// time_range_end intentionally omitted
		"justification": "Investigating policy violation under reference ABC-2026-001",
	}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, body, "compliance_officer", "sub-officer-1")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d — body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
	if !strings.Contains(resp["error"], "time_range_end") {
		t.Errorf("expected error to reference time_range_end, got %q", resp["error"])
	}
}

func TestPostAccessRequest_MalformedTimestamp_Returns400(t *testing.T) {
	db := openFakeDB(t, true)
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: &mockCoreClient{}}

	body := complianceRequest{
		RoomID:         "!abc123:server.example",
		TimeRangeStart: "not-a-timestamp", // fails time.Parse(RFC3339)
		TimeRangeEnd:   "2026-03-31T23:59:59Z",
		Justification:  "Investigating policy violation under reference ABC-2026-001",
	}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, body, "compliance_officer", "sub-officer-1")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d — body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if resp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode=M_BAD_JSON, got %q", resp["errcode"])
	}
}

// ─── MINOR-5: Audit-never-raise path ──────────────────────────────────────────
//
// AC5 specifies that audit failures MUST NOT block the response path.
// The mock CoreClient returns an error; the handler must still emit 201 Created
// with a valid JSON body so the caller observes a successful access request.
// This locks in the never-raise policy at the handler level (defence-in-depth
// alongside the audit.LogEvent internal never-raise).

func TestPostAccessRequest_AuditFailure_Still201(t *testing.T) {
	db := openFakeDB(t, true /* roomFound */)
	client := &mockCoreClient{failWith: fmt.Errorf("simulated gRPC audit failure")}
	h := &compliance.AccessRequestHandler{DB: db, CoreClient: client}

	w := httptest.NewRecorder()
	r := newJSONRequest(t, validBody(), "compliance_officer", "sub-officer-audit-fail")

	h.PostAccessRequest(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("audit failure must not block 201 — got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v — body: %s", err, w.Body.String())
	}
	if !uuidRegex.MatchString(resp["request_id"]) {
		t.Errorf("expected request_id to be a canonical UUID, got %q", resp["request_id"])
	}
	if resp["status"] != "pending" {
		t.Errorf("expected status=pending, got %q", resp["status"])
	}
	// Sanity: the audit call WAS attempted exactly once — we did not skip the audit path.
	if got := client.callCount(); got != 1 {
		t.Errorf("expected audit WriteAuditLog to be attempted once, got %d", got)
	}
}
