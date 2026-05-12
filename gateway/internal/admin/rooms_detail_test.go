package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// maxMembersLimit mirrors the upper bound enforced by UpdateRoomSettingsHandler
// in rooms.go (line 358). Tests reference it instead of duplicating the magic
// number, so a future limit change only requires updating one place.
const maxMembersLimit = 1_000_000

// TestRoomDetailPanelRenders verifies that GET /admin/rooms/room-001 returns HTTP 200
// with the room name and the inline_edit component rendered.
// AC: 8 (TestRoomDetailPanelRenders — Story 7.9)
func TestRoomDetailPanelRenders(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/rooms/{roomId}", h.DetailHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/rooms/room-001", nil)
	mux.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "General") {
		t.Error("expected body to contain 'General' (room name)")
	}
	if !strings.Contains(body, "inline-edit-field") {
		t.Error("expected body to contain 'inline-edit-field' (from inline_edit component)")
	}
}

// TestRoomDetailPanelNotFound verifies that GET /admin/rooms/xxx-999 returns HTTP 404.
// AC: 8 (TestRoomDetailPanelNotFound — Story 7.9); also AC1 (404 for unknown room).
func TestRoomDetailPanelNotFound(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/rooms/{roomId}", h.DetailHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/rooms/xxx-999", nil)
	mux.ServeHTTP(w, r)

	if w.Code != 404 {
		t.Fatalf("want 404 for unknown room got %d", w.Code)
	}
}

// TestRoomDetailFlashMessage verifies that ?flash=Room+name+updated renders
// an alert banner containing that text (canonical allowlist value, Story 7.18).
// AC: 8 (TestRoomDetailFlashMessage — Story 7.9)
func TestRoomDetailFlashMessage(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/rooms/{roomId}", h.DetailHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/rooms/room-001?flash=Room+name+updated", nil)
	mux.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Room name updated") {
		t.Errorf("expected body to contain 'Room name updated', got: %s", body[:min(500, len(body))])
	}
}

// TestUpdateRoomName verifies that POST /admin/rooms/room-001/name with a valid name
// redirects to the detail URL with a flash param.
// AC: 8 (TestUpdateRoomName — Story 7.9)
func TestUpdateRoomName(t *testing.T) {
	// Save original name by ID and restore in t.Cleanup.
	var originalName string
	for _, room := range stubRooms {
		if room.ID == "room-001" {
			originalName = room.Name
			break
		}
	}
	t.Cleanup(func() {
		for i := range stubRooms {
			if stubRooms[i].ID == "room-001" {
				stubRooms[i].Name = originalName
				break
			}
		}
	})

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/name", h.UpdateRoomNameHandler)

	form := url.Values{}
	form.Set("name", "New Room")
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/name", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "/admin/rooms/room-001") {
		t.Errorf("expected Location to contain '/admin/rooms/room-001', got: %s", location)
	}
	if !strings.Contains(location, "flash=") {
		t.Errorf("expected Location to contain 'flash=', got: %s", location)
	}
}

// TestUpdateRoomNameEmpty verifies that POST with an empty name returns HTTP 400.
// AC: 8 (TestUpdateRoomNameEmpty — Story 7.9)
func TestUpdateRoomNameEmpty(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/name", h.UpdateRoomNameHandler)

	form := url.Values{}
	form.Set("name", "")
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/name", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for empty name got %d", w.Code)
	}
}

// TestUpdateRoomNameTooLong verifies that POST with a 101-rune name returns HTTP 400.
// AC: 8 (TestUpdateRoomNameTooLong — Story 7.9)
func TestUpdateRoomNameTooLong(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/name", h.UpdateRoomNameHandler)

	form := url.Values{}
	form.Set("name", strings.Repeat("x", 101))
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/name", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for 101-rune name got %d", w.Code)
	}
}

// TestArchiveRoom verifies that POST /admin/rooms/room-002/archive returns HTTP 302
// with a flash= Location and mutates the stub status to "archived".
// Uses room-002 (Engineering) to avoid interfering with name-update tests on room-001.
// AC: 8 (TestArchiveRoom — Story 7.9)
func TestArchiveRoom(t *testing.T) {
	// Restore room-002 to "active" after the test using ID lookup (order-safe).
	t.Cleanup(func() {
		for i := range stubRooms {
			if stubRooms[i].ID == "room-002" {
				stubRooms[i].Status = "active"
				break
			}
		}
	})

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/archive", h.ArchiveRoomHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-002/archive", nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "flash=") {
		t.Errorf("expected Location to contain 'flash=', got: %s", location)
	}

	// Verify stub mutation: room-002 must now be "archived".
	room := findStubRoom("room-002")
	if room == nil {
		t.Fatal("room-002 not found in stubRooms after archive")
	}
	if room.Status != "archived" {
		t.Errorf("expected stubRooms room-002 Status == 'archived', got: %s", room.Status)
	}
}

// TestArchiveConfirmDialogRendered verifies that GET /admin/rooms/room-001 returns HTTP 200
// and the body contains the confirm_dialog component (role="alertdialog").
// AC: 8 (TestArchiveConfirmDialogRendered — Story 7.9)
func TestArchiveConfirmDialogRendered(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/rooms/{roomId}", h.DetailHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/rooms/room-001", nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `role="alertdialog"`) {
		t.Errorf("expected body to contain role=\"alertdialog\" (confirm_dialog component); got body length %d", len(body))
	}
}

// ---------------------------------------------------------------------------
// Story 9.17 GAP-9-002 — POST /admin/rooms/{roomId}/settings
// AC: 9.3-3 (UpdateRoomSettingsHandler — Story 9.17)
// ---------------------------------------------------------------------------

// TestUpdateRoomMaxMembers verifies that POST /admin/rooms/room-001/settings with max_members=50
// returns HTTP 302 with Location containing /admin/rooms/room-001 and flash=Settings+updated.
// AC: 9.3-3 (TestUpdateRoomMaxMembers — Story 9.17 GAP-9-002)
func TestUpdateRoomMaxMembers(t *testing.T) {
	t.Parallel()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

	form := url.Values{}
	form.Set("max_members", "50")
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/settings", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "/admin/rooms/room-001") {
		t.Errorf("expected Location to contain '/admin/rooms/room-001', got: %s", location)
	}
	if !strings.Contains(location, "flash=Settings+updated") {
		t.Errorf("expected Location to contain 'flash=Settings+updated', got: %s", location)
	}
}

// TestUpdateRoomMaxMembersZero verifies that POST with max_members=0 returns HTTP 302.
// Zero means "no limit" — it is a valid value and must not produce a 400 error.
// AC: 9.3-3 (TestUpdateRoomMaxMembersZero — Story 9.17 GAP-9-002)
func TestUpdateRoomMaxMembersZero(t *testing.T) {
	t.Parallel()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

	form := url.Values{}
	form.Set("max_members", "0")
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/settings", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 for max_members=0 (no limit) got %d", w.Code)
	}
}

// TestUpdateRoomMaxMembersNegative verifies that POST with max_members=-1 returns HTTP 400.
// Negative values are not valid member limits.
// AC: 9.3-3 (TestUpdateRoomMaxMembersNegative — Story 9.17 GAP-9-002)
func TestUpdateRoomMaxMembersNegative(t *testing.T) {
	t.Parallel()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

	form := url.Values{}
	form.Set("max_members", "-1")
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/settings", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for negative max_members got %d", w.Code)
	}
}

// TestUpdateRoomMaxMembersInvalid verifies that POST with max_members=abc returns HTTP 400.
// Non-numeric strings cannot be parsed and must be rejected.
// AC: 9.3-3 (TestUpdateRoomMaxMembersInvalid — Story 9.17 GAP-9-002)
func TestUpdateRoomMaxMembersInvalid(t *testing.T) {
	t.Parallel()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

	form := url.Values{}
	form.Set("max_members", "abc")
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/settings", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for non-numeric max_members got %d", w.Code)
	}
}

// TestUpdateRoomMaxMembersTooLarge verifies that POST with max_members=1000001 returns HTTP 400.
// Values above the 1_000_000 ceiling must be rejected.
// AC: 9.3-3 (TestUpdateRoomMaxMembersTooLarge — Story 9.17 GAP-9-002)
func TestUpdateRoomMaxMembersTooLarge(t *testing.T) {
	t.Parallel()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

	form := url.Values{}
	form.Set("max_members", strconv.Itoa(maxMembersLimit+1))
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/settings", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for max_members > %d got %d", maxMembersLimit, w.Code)
	}
}

// TestUpdateRoomMaxMembersAtLimit verifies that POST with max_members=1000000 returns HTTP 302.
// Exactly 1_000_000 is the valid upper boundary — it must be accepted.
// AC: 9.3-3 (TestUpdateRoomMaxMembersAtLimit — Story 9.17 GAP-9-002)
func TestUpdateRoomMaxMembersAtLimit(t *testing.T) {
	t.Parallel()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

	form := url.Values{}
	form.Set("max_members", strconv.Itoa(maxMembersLimit))
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/settings", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 for max_members=%d (valid boundary) got %d", maxMembersLimit, w.Code)
	}
}

// TestUpdateRoomSettingsWithVisibility verifies that sending a visibility field alongside
// max_members does not break the handler — visibility is silently ignored (not in proto).
// AC: 9.3-3 (TestUpdateRoomSettingsWithVisibility — Story 9.17 GAP-9-002)
func TestUpdateRoomSettingsWithVisibility(t *testing.T) {
	t.Parallel()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

	form := url.Values{}
	form.Set("max_members", "50")
	form.Set("visibility", "private")
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/settings", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 when visibility field is present got %d", w.Code)
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "flash=Settings+updated") {
		t.Errorf("expected Location to contain 'flash=Settings+updated', got: %s", location)
	}
}

// ---------------------------------------------------------------------------
// Story 9.18 — Room Detail: Member List
// RED PHASE: tests FAIL until implementation exists because:
//   - RoomMemberData struct does not exist in page_data.go (AC6)
//   - ActiveRoomMembers field does not exist on RoomsPageData (AC6)
//   - stubRoomMembers map does not exist in stubs.go (AC8)
//   - DetailHandler does not populate ActiveRoomMembers (AC5, AC8)
//   - rooms.html does not contain the Members section (AC7)
// ---------------------------------------------------------------------------

// TestRoomDetailMemberListRenders verifies that GET /admin/rooms/room-001 (stub path)
// returns HTTP 200 and renders the member list with display names and user links.
// AC9 (Story 9.18): room-001 has two entries in stubRoomMembers (Alice Müller, Carla Reiter).
func TestRoomDetailMemberListRenders(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/rooms/{roomId}", h.DetailHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/rooms/room-001", nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d\nbody: %s", w.Code, w.Body.String()[:min(500, w.Body.Len())])
	}
	body := w.Body.String()

	// AC9: body must contain "Alice Müller"
	if !strings.Contains(body, "Alice Müller") {
		t.Error("expected body to contain 'Alice Müller' (first stub member of room-001)")
	}
	// AC9: body must contain a link to /admin/users/usr-001
	if !strings.Contains(body, "/admin/users/usr-001") {
		t.Error("expected body to contain '/admin/users/usr-001' (member link for usr-001)")
	}
	// AC9: body must contain "Carla Reiter"
	if !strings.Contains(body, "Carla Reiter") {
		t.Error("expected body to contain 'Carla Reiter' (second stub member of room-001)")
	}
}

// TestRoomDetailNoMembers verifies that GET /admin/rooms/room-003 (stub path, room with
// no entry in stubRoomMembers) returns HTTP 200 and does NOT render the Members section.
// AC10 (Story 9.18): the {{ if .ActiveRoomMembers }} guard must suppress the section heading.
func TestRoomDetailNoMembers(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/rooms/{roomId}", h.DetailHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/rooms/room-003", nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 for room-003 (no members) got %d\nbody: %s", w.Code, w.Body.String()[:min(500, w.Body.Len())])
	}
	body := w.Body.String()

	// AC10: the Members section heading must NOT appear when there are no members
	if strings.Contains(body, "Members (") {
		t.Error("expected body NOT to contain 'Members (' for room with no stub members — {{ if .ActiveRoomMembers }} guard missing")
	}
}

// memberListGrpcErrorClient is a minimal AdminRoomsClient whose ListAdminRoomMembers
// always returns an error. GetAdminRoom returns a valid room; all other methods are no-ops.
// Used exclusively in TestRoomDetailMemberListGrpcError (AC5, Story 9.18).
type memberListGrpcErrorClient struct{}

func (memberListGrpcErrorClient) ListAdminRooms(_ context.Context, _ *pb.ListAdminRoomsRequest) (*pb.ListAdminRoomsResponse, error) {
	return &pb.ListAdminRoomsResponse{}, nil
}
func (memberListGrpcErrorClient) GetAdminRoom(_ context.Context, req *pb.GetAdminRoomRequest) (*pb.GetAdminRoomResponse, error) {
	return &pb.GetAdminRoomResponse{
		Room: &pb.AdminRoomDetailProto{RoomId: req.GetRoomId(), Name: "Test Room", Status: "active"},
	}, nil
}
func (memberListGrpcErrorClient) ArchiveRoom(_ context.Context, _ *pb.ArchiveRoomRequest) (*pb.ArchiveRoomResponse, error) {
	return &pb.ArchiveRoomResponse{}, nil
}
func (memberListGrpcErrorClient) UnarchiveRoom(_ context.Context, _ *pb.UnarchiveRoomRequest) (*pb.UnarchiveRoomResponse, error) {
	return &pb.UnarchiveRoomResponse{}, nil
}
func (memberListGrpcErrorClient) UpdateRoomSettings(_ context.Context, _ *pb.UpdateRoomSettingsRequest) (*pb.UpdateRoomSettingsResponse, error) {
	return &pb.UpdateRoomSettingsResponse{}, nil
}
func (memberListGrpcErrorClient) ListAdminRoomMembers(_ context.Context, _ *pb.ListAdminRoomMembersRequest) (*pb.ListAdminRoomMembersResponse, error) {
	return nil, fmt.Errorf("simulated gRPC error: transport: connection refused")
}

// TestRoomDetailMemberListGrpcError verifies AC5 (Story 9.18): when ListAdminRoomMembers
// returns a gRPC error, DetailHandler logs a warning and continues — returning HTTP 200
// without a Members section (non-fatal degradation).
func TestRoomDetailMemberListGrpcError(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl, memberListGrpcErrorClient{})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/rooms/{roomId}", h.DetailHandler)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/rooms/room-001", nil)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 on gRPC member-list error (non-fatal), got %d\nbody: %s",
			w.Code, w.Body.String()[:min(500, w.Body.Len())])
	}
	// Template guard {{ if .ActiveRoomMembers }} must suppress the section when list is empty.
	if strings.Contains(w.Body.String(), "Members (") {
		t.Error("expected 'Members (' to be absent when ListAdminRoomMembers returns an error")
	}
}

// TestUpdateRoomSettingsEmptyMaxMembers verifies that POST with an empty max_members string
// returns HTTP 302. Empty value means "no limit" and is not a validation error.
// AC: 9.3-3 (TestUpdateRoomSettingsEmptyMaxMembers — Story 9.17 GAP-9-002)
func TestUpdateRoomSettingsEmptyMaxMembers(t *testing.T) {
	t.Parallel()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := NewRoomsHandler(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

	form := url.Values{}
	form.Set("max_members", "")
	body := strings.NewReader(form.Encode())

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/admin/rooms/room-001/settings", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 for empty max_members (no-limit path) got %d", w.Code)
	}
}
