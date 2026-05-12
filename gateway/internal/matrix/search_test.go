package matrix

// Story 11.4: Gateway POST /_matrix/client/v3/search — Failing Acceptance Tests (Red Phase)
//
// AC1  — HTTP 200 with Matrix spec §11.14.1 response shape (count, results, next_batch, groups, highlights)
// AC2  — room_filter forwarded to gRPC SearchMessagesRequest.RoomFilter
// AC3  — results grouped by room_id in response.search_categories.room_events.groups
// AC4  — missing userID in context → 401 M_UNKNOWN_TOKEN (defense-in-depth)
// AC5  — gRPC ResourceExhausted → 429 M_LIMIT_EXCEEDED with retry_after_ms
// AC6  — gRPC PermissionDenied → 403 M_FORBIDDEN
// AC7  — gRPC Internal → 500 M_UNKNOWN
// AC8  — empty or whitespace search_term → 400 M_INVALID_PARAM
// AC9  — user_id forwarded via gRPC metadata (x-user-id), NOT set in SearchMessagesRequest.UserId
// AC10 — next_batch query param forwarded; absent from response when empty (last page)
//
// All tests compile against the SearchHandler interface but will fail at runtime
// until search.go is implemented (handler returns 501 or does not exist yet).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ─── Mock ──────────────────────────────────────────────────────────────────

// mockSearchCoreClient is a consumer-side double for SearchCoreClient.
// It records the last gRPC context and request so individual tests can
// inspect what the handler forwarded.
type mockSearchCoreClient struct {
	resp        *pb.SearchMessagesResponse
	err         error
	capturedCtx context.Context
	capturedReq *pb.SearchMessagesRequest
}

func (m *mockSearchCoreClient) SearchMessages(ctx context.Context, req *pb.SearchMessagesRequest) (*pb.SearchMessagesResponse, error) {
	m.capturedCtx = ctx
	m.capturedReq = req
	return m.resp, m.err
}

// ─── Helper ────────────────────────────────────────────────────────────────

// newSearchTestMux wires SearchHandler into a bare ServeMux at the Matrix path.
// The path does not include the /_matrix/client/v3 prefix so that httptest
// requests can use a short path (matching the pattern used by event_context_test.go).
func newSearchTestMux(mock *mockSearchCoreClient) *http.ServeMux {
	h := NewSearchHandler(SearchConfig{CoreClient: mock})
	mux := http.NewServeMux()
	mux.Handle("POST /search", http.HandlerFunc(h.PostSearch))
	return mux
}

// makeSearchRequest builds an httptest POST /search request.
// If userID is non-empty it injects user context (simulating jwtMiddleware).
// query is appended as a raw query string if non-empty.
func makeSearchRequest(mux *http.ServeMux, body, userID, role, query string) *httptest.ResponseRecorder {
	path := "/search"
	if query != "" {
		path += "?" + query
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req = req.WithContext(contextWithUser(req.Context(), userID, role))
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

// ─── AC1: Happy path ───────────────────────────────────────────────────────

// TestPostSearch_HappyPath verifies that a valid search request returns HTTP 200
// with the full Matrix spec §11.14.1 response shape.
func TestPostSearch_HappyPath(t *testing.T) {
	// Two search results — event JSON as raw bytes (Core serialises via Jason.encode!)
	event1JSON := []byte(`{"event_id":"$evt1:test.local","room_id":"!room1:test.local","sender":"@bob:test.local","type":"m.room.message","content":{"msgtype":"m.text","body":"hello world"}}`)
	event2JSON := []byte(`{"event_id":"$evt2:test.local","room_id":"!room2:test.local","sender":"@alice:test.local","type":"m.room.message","content":{"msgtype":"m.text","body":"hello there"}}`)

	mock := &mockSearchCoreClient{
		resp: &pb.SearchMessagesResponse{
			TotalCount: 2,
			NextBatch:  "dGVzdA==",
			Results: []*pb.SearchResult{
				{
					Rank:         0.9,
					Event:        event1JSON,
					EventsBefore: [][]byte{},
					EventsAfter:  [][]byte{},
					ProfileInfo:  map[string]*pb.ProfileInfo{},
				},
				{
					Rank:         0.7,
					Event:        event2JSON,
					EventsBefore: [][]byte{},
					EventsAfter:  [][]byte{},
					ProfileInfo:  map[string]*pb.ProfileInfo{},
				},
			},
		},
	}

	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":"hello","order_by":"rank"}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("AC1: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("AC1: expected Content-Type application/json, got %s", ct)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("AC1: invalid JSON: %v (body: %s)", err, rr.Body.String())
	}
	if _, ok := resp["search_categories"]; !ok {
		t.Fatal("AC1: missing top-level 'search_categories' in response")
	}

	var cats struct {
		RoomEvents struct {
			Count     int                      `json:"count"`
			Results   []map[string]interface{} `json:"results"`
			NextBatch string                   `json:"next_batch"`
			Groups    map[string]interface{}   `json:"groups"`
			Highlights []string               `json:"highlights"`
		} `json:"room_events"`
	}
	if err := json.Unmarshal(resp["search_categories"], &cats); err != nil {
		t.Fatalf("AC1: cannot parse search_categories: %v", err)
	}

	if cats.RoomEvents.Count != 2 {
		t.Errorf("AC1: expected count=2, got %d", cats.RoomEvents.Count)
	}
	if len(cats.RoomEvents.Results) != 2 {
		t.Errorf("AC1: expected 2 results, got %d", len(cats.RoomEvents.Results))
	}
	if cats.RoomEvents.NextBatch != "dGVzdA==" {
		t.Errorf("AC1: expected next_batch='dGVzdA==', got %q", cats.RoomEvents.NextBatch)
	}

	// Each result must have rank, result (event), and context with required sub-fields (MINOR-2 fix).
	for i, r := range cats.RoomEvents.Results {
		if _, ok := r["rank"]; !ok {
			t.Errorf("AC1: result[%d] missing 'rank'", i)
		}
		if _, ok := r["result"]; !ok {
			t.Errorf("AC1: result[%d] missing 'result'", i)
		}
		ctx, ok := r["context"].(map[string]interface{})
		if !ok {
			t.Errorf("AC1: result[%d]['context'] is missing or not an object", i)
			continue
		}
		if _, ok := ctx["events_before"]; !ok {
			t.Errorf("AC1: result[%d]['context'] missing 'events_before'", i)
		}
		if _, ok := ctx["events_after"]; !ok {
			t.Errorf("AC1: result[%d]['context'] missing 'events_after'", i)
		}
		if _, ok := ctx["profile_info"]; !ok {
			t.Errorf("AC1: result[%d]['context'] missing 'profile_info'", i)
		}
	}

	// highlights must be present and contain the search_term words (MINOR-1 fix).
	// search_term "hello" → highlights ["hello"].
	if cats.RoomEvents.Highlights == nil {
		t.Error("AC1: highlights must not be nil (Matrix spec requires the field)")
	}
	if len(cats.RoomEvents.Highlights) == 0 {
		t.Error("AC1: highlights must contain search_term words, got empty slice")
	}
}

// ─── AC2: room_filter forwarded ────────────────────────────────────────────

// TestPostSearch_RoomFilter_Forwarded verifies that filter.rooms from the request
// is forwarded as SearchMessagesRequest.RoomFilter to the gRPC call.
func TestPostSearch_RoomFilter_Forwarded(t *testing.T) {
	mock := &mockSearchCoreClient{
		resp: &pb.SearchMessagesResponse{TotalCount: 0, Results: []*pb.SearchResult{}},
	}

	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":"hello","filter":{"rooms":["!room1:test.local"]}}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("AC2: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if mock.capturedReq == nil {
		t.Fatal("AC2: expected SearchMessages to be called, capturedReq is nil")
	}
	if len(mock.capturedReq.RoomFilter) != 1 || mock.capturedReq.RoomFilter[0] != "!room1:test.local" {
		t.Errorf("AC2: expected RoomFilter=['!room1:test.local'], got %v", mock.capturedReq.RoomFilter)
	}
}

// ─── AC3: grouping by room_id ──────────────────────────────────────────────

// TestPostSearch_GroupsByRoomID verifies that results are grouped by room_id
// in response.search_categories.room_events.groups.
func TestPostSearch_GroupsByRoomID(t *testing.T) {
	event1JSON := []byte(`{"event_id":"$evt1:test.local","room_id":"!roomA:test.local","sender":"@bob:test.local","type":"m.room.message"}`)
	event2JSON := []byte(`{"event_id":"$evt2:test.local","room_id":"!roomA:test.local","sender":"@bob:test.local","type":"m.room.message"}`)
	event3JSON := []byte(`{"event_id":"$evt3:test.local","room_id":"!roomB:test.local","sender":"@alice:test.local","type":"m.room.message"}`)

	mock := &mockSearchCoreClient{
		resp: &pb.SearchMessagesResponse{
			TotalCount: 3,
			Results: []*pb.SearchResult{
				{Rank: 0.9, Event: event1JSON, EventsBefore: [][]byte{}, EventsAfter: [][]byte{}},
				{Rank: 0.8, Event: event2JSON, EventsBefore: [][]byte{}, EventsAfter: [][]byte{}},
				{Rank: 0.7, Event: event3JSON, EventsBefore: [][]byte{}, EventsAfter: [][]byte{}},
			},
		},
	}

	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":"hello"}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("AC3: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		SearchCategories struct {
			RoomEvents struct {
				Groups map[string]interface{} `json:"groups"`
			} `json:"room_events"`
		} `json:"search_categories"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("AC3: invalid JSON: %v", err)
	}

	groups := resp.SearchCategories.RoomEvents.Groups
	if groups == nil {
		t.Fatal("AC3: groups is nil in response")
	}
	roomGroups, ok := groups["room_id"].(map[string]interface{})
	if !ok {
		t.Fatalf("AC3: groups[\"room_id\"] is missing or wrong type, got: %T %v", groups["room_id"], groups["room_id"])
	}
	if _, ok := roomGroups["!roomA:test.local"]; !ok {
		t.Error("AC3: expected group entry for !roomA:test.local")
	}
	if _, ok := roomGroups["!roomB:test.local"]; !ok {
		t.Error("AC3: expected group entry for !roomB:test.local")
	}
}

// ─── AC4: unauthenticated ──────────────────────────────────────────────────

// TestPostSearch_Unauthenticated verifies that a request without a userID in context
// (defense-in-depth: jwtMiddleware has already rejected before the handler normally)
// returns 401 M_UNKNOWN_TOKEN.
func TestPostSearch_Unauthenticated(t *testing.T) {
	mock := &mockSearchCoreClient{}
	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":"hello"}}}`

	// No userID injected into context — simulates the handler being called without auth.
	rr := makeSearchRequest(mux, body, "", "", "")

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("AC4: expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_UNKNOWN_TOKEN") {
		t.Errorf("AC4: expected M_UNKNOWN_TOKEN, got: %s", rr.Body.String())
	}
}

// ─── AC5: gRPC ResourceExhausted → 429 ─────────────────────────────────────

// TestPostSearch_ResourceExhausted_429 verifies that a gRPC ResourceExhausted error
// maps to HTTP 429 M_LIMIT_EXCEEDED with a retry_after_ms field.
func TestPostSearch_ResourceExhausted_429(t *testing.T) {
	mock := &mockSearchCoreClient{
		err: status.Error(codes.ResourceExhausted, "rate limit exceeded"),
	}

	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":"hello"}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("AC5: expected 429, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_LIMIT_EXCEEDED") {
		t.Errorf("AC5: expected M_LIMIT_EXCEEDED, got: %s", rr.Body.String())
	}

	var errBody map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("AC5: invalid JSON in 429 body: %v", err)
	}
	retryAfter, ok := errBody["retry_after_ms"]
	if !ok {
		t.Error("AC5: missing 'retry_after_ms' in 429 response body")
	} else {
		// retry_after_ms must be a positive number (JSON numbers decode to float64).
		if v, ok := retryAfter.(float64); !ok || v <= 0 {
			t.Errorf("AC5: retry_after_ms must be a positive number, got %v", retryAfter)
		}
	}
}

// ─── AC6: gRPC PermissionDenied → 403 ─────────────────────────────────────

// TestPostSearch_PermissionDenied_403 verifies that a gRPC PermissionDenied error
// maps to HTTP 403 M_FORBIDDEN.
func TestPostSearch_PermissionDenied_403(t *testing.T) {
	mock := &mockSearchCoreClient{
		err: status.Error(codes.PermissionDenied, "not a member"),
	}

	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":"hello"}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusForbidden {
		t.Fatalf("AC6: expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_FORBIDDEN") {
		t.Errorf("AC6: expected M_FORBIDDEN, got: %s", rr.Body.String())
	}
}

// ─── AC7: gRPC Internal → 500 ─────────────────────────────────────────────

// TestPostSearch_InternalError_500 verifies that an unexpected gRPC error maps
// to HTTP 500 M_UNKNOWN.
func TestPostSearch_InternalError_500(t *testing.T) {
	mock := &mockSearchCoreClient{
		err: status.Error(codes.Internal, "search failed"),
	}

	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":"hello"}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("AC7: expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_UNKNOWN") {
		t.Errorf("AC7: expected M_UNKNOWN, got: %s", rr.Body.String())
	}
}

// ─── AC8: empty search_term ────────────────────────────────────────────────

// TestPostSearch_EmptySearchTerm verifies that an empty search_term returns
// 400 M_INVALID_PARAM without calling gRPC.
func TestPostSearch_EmptySearchTerm(t *testing.T) {
	mock := &mockSearchCoreClient{}
	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":""}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("AC8: expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_INVALID_PARAM") {
		t.Errorf("AC8: expected M_INVALID_PARAM, got: %s", rr.Body.String())
	}
	// gRPC must NOT be called for invalid input.
	if mock.capturedReq != nil {
		t.Error("AC8: SearchMessages must not be called for empty search_term")
	}
}

// TestPostSearch_WhitespaceSearchTerm verifies that a whitespace-only search_term
// is also rejected as 400 M_INVALID_PARAM.
func TestPostSearch_WhitespaceSearchTerm(t *testing.T) {
	mock := &mockSearchCoreClient{}
	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":"   "}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("AC8 (whitespace): expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_INVALID_PARAM") {
		t.Errorf("AC8 (whitespace): expected M_INVALID_PARAM, got: %s", rr.Body.String())
	}
}

// TestPostSearch_MissingSearchCategories verifies that a request body without
// search_categories returns 400 M_MISSING_PARAM.
func TestPostSearch_MissingSearchCategories(t *testing.T) {
	mock := &mockSearchCoreClient{}
	mux := newSearchTestMux(mock)
	body := `{}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("AC8 (missing categories): expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	// Missing top-level required object → M_MISSING_PARAM or M_INVALID_PARAM per Matrix spec.
	bodyStr := rr.Body.String()
	if !strings.Contains(bodyStr, "M_MISSING_PARAM") && !strings.Contains(bodyStr, "M_INVALID_PARAM") {
		t.Errorf("AC8 (missing categories): expected M_MISSING_PARAM or M_INVALID_PARAM, got: %s", bodyStr)
	}
}

// TestPostSearch_MissingRoomEvents verifies that search_categories without
// room_events returns 400 M_INVALID_PARAM.
func TestPostSearch_MissingRoomEvents(t *testing.T) {
	mock := &mockSearchCoreClient{}
	mux := newSearchTestMux(mock)
	body := `{"search_categories":{}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("AC8 (missing room_events): expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_INVALID_PARAM") {
		t.Errorf("AC8 (missing room_events): expected M_INVALID_PARAM, got: %s", rr.Body.String())
	}
}

// TestPostSearch_MalformedJSON verifies that malformed JSON returns 400 M_NOT_JSON.
func TestPostSearch_MalformedJSON(t *testing.T) {
	mock := &mockSearchCoreClient{}
	mux := newSearchTestMux(mock)

	body := `not valid json {`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("AC8 (malformed JSON): expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "M_NOT_JSON") {
		t.Errorf("AC8 (malformed JSON): expected M_NOT_JSON, got: %s", rr.Body.String())
	}
}

// ─── AC9: user_id from context, NOT from body (SECURITY) ──────────────────

// TestPostSearch_UserIDFromContext_NotFromBody is the security regression test for
// the trusted-identity pattern (MEDIUM-2 from Kassandra review).
//
// It asserts two things:
//  1. SearchMessagesRequest.UserId is NOT set by the Gateway handler (empty string).
//  2. The gRPC outgoing context carries x-user-id = the authenticated user's ID
//     (forwarded via coregrpc.WithUserMetadata, not from any client-supplied field).
func TestPostSearch_UserIDFromContext_NotFromBody(t *testing.T) {
	mock := &mockSearchCoreClient{
		resp: &pb.SearchMessagesResponse{TotalCount: 0, Results: []*pb.SearchResult{}},
	}

	mux := newSearchTestMux(mock)
	// The Matrix search request body has no user_id field — it's not part of the spec.
	body := `{"search_categories":{"room_events":{"search_term":"secure"}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("AC9: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if mock.capturedReq == nil {
		t.Fatal("AC9: expected SearchMessages to be called, capturedReq is nil")
	}

	// SECURITY: The handler MUST NOT set UserId on the proto request.
	// Core ignores this field, but setting it indicates a conceptual trust-boundary error.
	if mock.capturedReq.UserId != "" {
		t.Errorf("AC9 SECURITY: SearchMessagesRequest.UserId MUST be empty string (not %q); user_id must come from x-user-id gRPC metadata only", mock.capturedReq.UserId)
	}

	// The gRPC context MUST carry x-user-id metadata set by WithUserMetadata.
	if mock.capturedCtx == nil {
		t.Fatal("AC9: captured gRPC context is nil")
	}
	md, ok := metadata.FromOutgoingContext(mock.capturedCtx)
	if !ok {
		t.Fatal("AC9 SECURITY: no outgoing gRPC metadata found — WithUserMetadata was not called")
	}
	vals := md.Get("x-user-id")
	if len(vals) == 0 {
		t.Fatal("AC9 SECURITY: x-user-id metadata key is absent — WithUserMetadata was not called")
	}
	if vals[0] != "@alice:test.local" {
		t.Errorf("AC9 SECURITY: x-user-id metadata = %q, want %q", vals[0], "@alice:test.local")
	}
}

// ─── AC10: next_batch pagination ───────────────────────────────────────────

// TestPostSearch_NextBatch_QueryParam_Forwarded verifies that next_batch in the
// query string is forwarded to SearchMessagesRequest.NextBatch.
func TestPostSearch_NextBatch_QueryParam_Forwarded(t *testing.T) {
	mock := &mockSearchCoreClient{
		resp: &pb.SearchMessagesResponse{TotalCount: 0, Results: []*pb.SearchResult{}},
	}

	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":"hello"}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "next_batch=dGVzdA%3D%3D")

	if rr.Code != http.StatusOK {
		t.Fatalf("AC10: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if mock.capturedReq == nil {
		t.Fatal("AC10: expected SearchMessages to be called, capturedReq is nil")
	}
	if mock.capturedReq.NextBatch != "dGVzdA==" {
		t.Errorf("AC10: expected NextBatch='dGVzdA==', got %q", mock.capturedReq.NextBatch)
	}
}

// TestPostSearch_NextBatch_AbsentOnLastPage verifies that next_batch is absent
// from the response JSON when the gRPC response has an empty NextBatch (last page).
// Per Matrix spec, clients interpret absent next_batch as "no more pages".
func TestPostSearch_NextBatch_AbsentOnLastPage(t *testing.T) {
	mock := &mockSearchCoreClient{
		resp: &pb.SearchMessagesResponse{
			TotalCount: 1,
			NextBatch:  "", // empty = no more pages
			Results: []*pb.SearchResult{
				{
					Rank:  0.9,
					Event: []byte(`{"event_id":"$evt1:test.local","room_id":"!r:test.local","sender":"@bob:test.local","type":"m.room.message"}`),
				},
			},
		},
	}

	mux := newSearchTestMux(mock)
	body := `{"search_categories":{"room_events":{"search_term":"hello"}}}`
	rr := makeSearchRequest(mux, body, "@alice:test.local", "user", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("AC10 (last page): expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Parse as a generic map so we can check key absence.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("AC10 (last page): invalid JSON: %v", err)
	}

	var cats struct {
		RoomEvents map[string]json.RawMessage `json:"room_events"`
	}
	if err := json.Unmarshal(raw["search_categories"], &cats); err != nil {
		t.Fatalf("AC10 (last page): cannot parse search_categories: %v", err)
	}

	// MINOR-3 fix: next_batch MUST be absent (omitempty) when gRPC returns empty string.
	// Matrix clients treat absent next_batch as "no more pages" — "next_batch":""  is a spec violation.
	if _, ok := cats.RoomEvents["next_batch"]; ok {
		t.Errorf("AC10 (last page): next_batch MUST be absent from JSON when no more pages (omitempty), but key is present in response")
	}
}
