package admin

// Tests for HIGH security fix: admin gRPC handlers must forward the admin user's
// identity as gRPC metadata ("x-user-id") so that Core audit log entries record
// a non-nil actor_user_id.
//
// Previously all state-changing admin handlers passed r.Context() directly to
// gRPC calls, causing Core's Nebu.Grpc.Metadata.user_id(stream) to return nil.
// Now they call contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))
// which sets the "x-user-id" metadata key before the gRPC call.
//
// Each test:
//   1. Builds a fake gRPC client that captures the outgoing context's metadata.
//   2. Sets contextKeyAdminSub in the request context (simulating SessionGuard).
//   3. Calls the handler.
//   4. Asserts that "x-user-id" metadata in the captured context matches the
//      admin user ID that was stored in the request context.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"google.golang.org/grpc/metadata"
)

// ---------------------------------------------------------------------------
// captureContextClient — records the context passed to each state-changing RPC
// so tests can inspect the outgoing gRPC metadata.
// ---------------------------------------------------------------------------

type captureContextClient struct {
	lastCtx context.Context
	// embed no-op implementations for the full AdminUsersClient + AdminRoomsClient interfaces
}

// AdminUsersClient methods

func (c *captureContextClient) ListAdminUsers(ctx context.Context, _ *pb.ListAdminUsersRequest) (*pb.ListAdminUsersResponse, error) {
	return &pb.ListAdminUsersResponse{}, nil
}
func (c *captureContextClient) GetAdminUser(ctx context.Context, _ *pb.GetAdminUserRequest) (*pb.GetAdminUserResponse, error) {
	return &pb.GetAdminUserResponse{}, nil
}
func (c *captureContextClient) DeactivateUser(ctx context.Context, _ *pb.DeactivateUserRequest) (*pb.DeactivateUserResponse, error) {
	c.lastCtx = ctx
	return &pb.DeactivateUserResponse{Ok: true}, nil
}
func (c *captureContextClient) ReactivateUser(ctx context.Context, _ *pb.ReactivateUserRequest) (*pb.ReactivateUserResponse, error) {
	c.lastCtx = ctx
	return &pb.ReactivateUserResponse{Ok: true}, nil
}
func (c *captureContextClient) UpdateUserRole(ctx context.Context, _ *pb.UpdateUserRoleRequest) (*pb.UpdateUserRoleResponse, error) {
	c.lastCtx = ctx
	return &pb.UpdateUserRoleResponse{Ok: true}, nil
}

// AdminRoomsClient methods

func (c *captureContextClient) ListAdminRooms(ctx context.Context, _ *pb.ListAdminRoomsRequest) (*pb.ListAdminRoomsResponse, error) {
	return &pb.ListAdminRoomsResponse{}, nil
}
func (c *captureContextClient) GetAdminRoom(ctx context.Context, _ *pb.GetAdminRoomRequest) (*pb.GetAdminRoomResponse, error) {
	return &pb.GetAdminRoomResponse{}, nil
}
func (c *captureContextClient) ArchiveRoom(ctx context.Context, _ *pb.ArchiveRoomRequest) (*pb.ArchiveRoomResponse, error) {
	c.lastCtx = ctx
	return &pb.ArchiveRoomResponse{Ok: true}, nil
}
func (c *captureContextClient) UnarchiveRoom(ctx context.Context, _ *pb.UnarchiveRoomRequest) (*pb.UnarchiveRoomResponse, error) {
	c.lastCtx = ctx
	return &pb.UnarchiveRoomResponse{Ok: true}, nil
}
func (c *captureContextClient) UpdateRoomSettings(ctx context.Context, _ *pb.UpdateRoomSettingsRequest) (*pb.UpdateRoomSettingsResponse, error) {
	c.lastCtx = ctx
	return &pb.UpdateRoomSettingsResponse{Ok: true}, nil
}

// ---------------------------------------------------------------------------
// Helper: build a request context that simulates SessionGuard having validated
// the admin session and stored the admin user's sub/ID in the context.
// ---------------------------------------------------------------------------

func requestWithAdminSub(method, target, body string, adminUserID string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	// Inject the admin user ID into the context exactly as SessionGuard does.
	ctx := context.WithValue(r.Context(), contextKeyAdminSub, adminUserID)
	return r.WithContext(ctx)
}

// assertXUserID extracts the "x-user-id" metadata value from a captured context
// and asserts it equals want. Fails the test if the metadata is absent or wrong.
func assertXUserID(t *testing.T, ctx context.Context, want string) {
	t.Helper()
	if ctx == nil {
		t.Fatal("captured context is nil — gRPC call was not made")
	}
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("outgoing gRPC metadata not set on captured context")
	}
	vals := md.Get("x-user-id")
	if len(vals) == 0 {
		t.Fatal("x-user-id metadata key is absent in gRPC context")
	}
	if vals[0] != want {
		t.Errorf("x-user-id: want %q, got %q", want, vals[0])
	}
}

// assertSystemRole extracts the "x-system-role" metadata value and asserts it equals want.
func assertSystemRole(t *testing.T, ctx context.Context, want string) {
	t.Helper()
	if ctx == nil {
		t.Fatal("captured context is nil — gRPC call was not made")
	}
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("outgoing gRPC metadata not set on captured context")
	}
	vals := md.Get("x-system-role")
	if len(vals) == 0 {
		t.Fatal("x-system-role metadata key is absent in gRPC context")
	}
	if vals[0] != want {
		t.Errorf("x-system-role: want %q, got %q", want, vals[0])
	}
}

// ---------------------------------------------------------------------------
// Tests for UsersHandler
// ---------------------------------------------------------------------------

// TestDeactivateUser_ForwardsAdminIdentityToGRPC verifies that POST /admin/users/{userId}/deactivate
// sets x-user-id = admin's sub in the outgoing gRPC context (HIGH security fix).
func TestDeactivateUser_ForwardsAdminIdentityToGRPC(t *testing.T) {
	const adminID = "@alice:nebu.example.com"

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	fake := &captureContextClient{}
	h := NewUsersHandler(tmpl, fake)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/users/{userId}/deactivate", h.DeactivateUserHandler)

	w := httptest.NewRecorder()
	r := requestWithAdminSub(http.MethodPost, "/admin/users/usr-003/deactivate", "", adminID)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	assertXUserID(t, fake.lastCtx, adminID)
	assertSystemRole(t, fake.lastCtx, "instance_admin")
}

// TestReactivateUser_ForwardsAdminIdentityToGRPC verifies that POST /admin/users/{userId}/reactivate
// sets x-user-id = admin's sub in the outgoing gRPC context (HIGH security fix).
func TestReactivateUser_ForwardsAdminIdentityToGRPC(t *testing.T) {
	const adminID = "@alice:nebu.example.com"

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	fake := &captureContextClient{}
	h := NewUsersHandler(tmpl, fake)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/users/{userId}/reactivate", h.ReactivateUserHandler)

	w := httptest.NewRecorder()
	r := requestWithAdminSub(http.MethodPost, "/admin/users/usr-003/reactivate", "", adminID)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	assertXUserID(t, fake.lastCtx, adminID)
	assertSystemRole(t, fake.lastCtx, "instance_admin")
}

// TestUpdateUserRole_ForwardsAdminIdentityToGRPC verifies that POST /admin/users/{userId}/role
// sets x-user-id = admin's sub in the outgoing gRPC context (HIGH security fix).
func TestUpdateUserRole_ForwardsAdminIdentityToGRPC(t *testing.T) {
	const adminID = "@alice:nebu.example.com"

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	fake := &captureContextClient{}
	h := NewUsersHandler(tmpl, fake)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/users/{userId}/role", h.UpdateRoleHandler)

	form := url.Values{}
	form.Set("role", "user")
	w := httptest.NewRecorder()
	r := requestWithAdminSub(http.MethodPost, "/admin/users/usr-001/role", form.Encode(), adminID)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	assertXUserID(t, fake.lastCtx, adminID)
	assertSystemRole(t, fake.lastCtx, "instance_admin")
}

// ---------------------------------------------------------------------------
// Tests for RoomsHandler
// ---------------------------------------------------------------------------

// TestArchiveRoom_ForwardsAdminIdentityToGRPC verifies that POST /admin/rooms/{roomId}/archive
// sets x-user-id = admin's sub in the outgoing gRPC context (HIGH security fix).
func TestArchiveRoom_ForwardsAdminIdentityToGRPC(t *testing.T) {
	const adminID = "@alice:nebu.example.com"

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	fake := &captureContextClient{}
	h := NewRoomsHandler(tmpl, fake)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/archive", h.ArchiveRoomHandler)

	w := httptest.NewRecorder()
	r := requestWithAdminSub(http.MethodPost, "/admin/rooms/room-002/archive", "", adminID)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	assertXUserID(t, fake.lastCtx, adminID)
	assertSystemRole(t, fake.lastCtx, "instance_admin")
}

// TestUnarchiveRoom_ForwardsAdminIdentityToGRPC verifies that POST /admin/rooms/{roomId}/unarchive
// sets x-user-id = admin's sub in the outgoing gRPC context (HIGH security fix).
func TestUnarchiveRoom_ForwardsAdminIdentityToGRPC(t *testing.T) {
	const adminID = "@alice:nebu.example.com"

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	fake := &captureContextClient{}
	h := NewRoomsHandler(tmpl, fake)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/unarchive", h.UnarchiveRoomHandler)

	w := httptest.NewRecorder()
	r := requestWithAdminSub(http.MethodPost, "/admin/rooms/room-002/unarchive", "", adminID)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	assertXUserID(t, fake.lastCtx, adminID)
	assertSystemRole(t, fake.lastCtx, "instance_admin")
}

// TestUpdateRoomSettings_ForwardsAdminIdentityToGRPC verifies that
// POST /admin/rooms/{roomId}/settings sets x-user-id = admin's sub in the
// outgoing gRPC context (HIGH security fix).
func TestUpdateRoomSettings_ForwardsAdminIdentityToGRPC(t *testing.T) {
	const adminID = "@alice:nebu.example.com"

	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	fake := &captureContextClient{}
	h := NewRoomsHandler(tmpl, fake)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/rooms/{roomId}/settings", h.UpdateRoomSettingsHandler)

	form := url.Values{}
	form.Set("max_members", "50")
	w := httptest.NewRecorder()
	r := requestWithAdminSub(http.MethodPost, "/admin/rooms/room-001/settings", form.Encode(), adminID)
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("want 302 got %d", w.Code)
	}
	assertXUserID(t, fake.lastCtx, adminID)
	assertSystemRole(t, fake.lastCtx, "instance_admin")
}
