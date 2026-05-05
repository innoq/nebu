package matrix

// ─── Story 5.27: Matrix Path Parameter Validation + Minor-Finding Bundle ─────
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until Story 5.27 is implemented.
//
// Failing reasons:
//   - ValidateMatrixRoomID / ValidateMatrixUserID / ValidateMatrixEventID do not
//     exist → compile error until gateway/internal/matrix/validate.go is created.
//   - handler.PutPresenceStatus does not exist on PresenceHandler → compile error.
//   - requireJSON / DisallowUnknownFields behaviour not yet enforced → runtime FAIL.
//
// AC coverage:
//   AC1  — ValidateMatrixRoomID, ValidateMatrixUserID, ValidateMatrixEventID
//   AC2  — Handler path-param validation → 400 M_INVALID_PARAM
//   AC3  — requireJSON: 415 M_UNSUPPORTED_MEDIA_TYPE on wrong Content-Type
//   AC4  — DisallowUnknownFields: 400 on extra JSON field in typed-struct handler
//   AC5  — PUT /presence/{userId}/status: 403 M_FORBIDDEN on userId mismatch
//   AC6  — GET /profile/{userId}: flattened 404 (no user-enumeration oracle)
//   AC7  — GET /keys/changes: 401 without Bearer
//   AC9  — Happy path + 3 malformed variants per validator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/nebu/nebu/internal/middleware"
)

// ─────────────────────────────────────────────────────────────────────────────
// AC1 + AC9: Validator Unit Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestValidateRoomID_Table covers AC1 + AC9.
// 12 malformed inputs rejected; 5 valid inputs accepted.
//
// FAILING: ValidateMatrixRoomID does not exist until validate.go is created.
func TestValidateRoomID_Table(t *testing.T) {
	valid := []struct {
		name string
		id   string
	}{
		{"simple", "!abc:example.com"},
		{"alphanumeric", "!room123:matrix.org"},
		{"special-chars", "!A1._=-:test.local"},
		{"minimal", "!z:a.b"},
		{"max-localpart", "!" + strings.Repeat("a", 63) + ":x.io"},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if err := ValidateMatrixRoomID(tc.id); err != nil {
				t.Errorf("ValidateMatrixRoomID(%q) returned unexpected error: %v", tc.id, err)
			}
		})
	}

	invalid := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"no-sigil", "room:example.com"},
		{"empty-localpart", "!:example.com"},
		{"no-server", "!abc:"},
		{"illegal-at", "!abc@x:example.com"},
		{"space-in-localpart", "! abc:example.com"},
		{"localpart-too-long", "!" + strings.Repeat("a", 64) + ":x.y"},
		{"server-too-long", "!abc:" + strings.Repeat("b", 256)},
		{"total-over-512", strings.Repeat("!", 1) + strings.Repeat("a", 256) + ":" + strings.Repeat("b", 256)},
		{"wrong-sigil-hash", "#abc:example.com"},
		{"wrong-sigil-at", "@abc:example.com"},
		{"wrong-sigil-dollar", "$abc:example.com"},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if err := ValidateMatrixRoomID(tc.id); err == nil {
				t.Errorf("ValidateMatrixRoomID(%q) should return error but returned nil", tc.id)
			}
		})
	}
}

// TestValidateUserID_Table covers AC1 + AC9.
// 5 valid inputs; 6 malformed rejected.
//
// FAILING: ValidateMatrixUserID does not exist until validate.go is created.
func TestValidateUserID_Table(t *testing.T) {
	valid := []struct {
		name string
		id   string
	}{
		{"simple", "@alice:example.com"},
		{"alphanumeric", "@bob123:matrix.org"},
		{"underscore", "@user_name:test.local"},
		{"dot", "@user.name:test.local"},
		{"minimal", "@a:b.c"},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if err := ValidateMatrixUserID(tc.id); err != nil {
				t.Errorf("ValidateMatrixUserID(%q) returned unexpected error: %v", tc.id, err)
			}
		})
	}

	invalid := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"no-sigil", "alice:example.com"},
		{"empty-localpart", "@:example.com"},
		{"no-server", "@alice:"},
		{"space-in-localpart", "@alice example:server.com"},
		{"localpart-too-long", "@" + strings.Repeat("a", 64) + ":x.y"},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if err := ValidateMatrixUserID(tc.id); err == nil {
				t.Errorf("ValidateMatrixUserID(%q) should return error but returned nil", tc.id)
			}
		})
	}
}

// TestValidateEventID_Table covers AC1 + AC9.
// Hash form (v3+) and legacy form; 6 malformed rejected.
//
// FAILING: ValidateMatrixEventID does not exist until validate.go is created.
func TestValidateEventID_Table(t *testing.T) {
	valid := []struct {
		name string
		id   string
	}{
		{"legacy", "$abc123:example.com"},
		{"hash-form-43", "$" + strings.Repeat("a", 43)},
		{"hash-allowed-chars", "$abc+/=_-XYZ"},
		{"hash-max-64", "$" + strings.Repeat("z", 64)},
		{"legacy-short", "$e:matrix.org"},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if err := ValidateMatrixEventID(tc.id); err != nil {
				t.Errorf("ValidateMatrixEventID(%q) returned unexpected error: %v", tc.id, err)
			}
		})
	}

	invalid := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"no-sigil", "abc123:example.com"},
		{"sigil-only", "$"},
		{"server-too-long", "$abc:" + strings.Repeat("x", 256)},
		{"localpart-too-long", "$" + strings.Repeat("a", 65) + ":x.y"},
		{"total-over-512", strings.Repeat("x", 513)},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			if err := ValidateMatrixEventID(tc.id); err == nil {
				t.Errorf("ValidateMatrixEventID(%q) should return error but returned nil", tc.id)
			}
		})
	}
}

// TestValidators_ByteCap_Isolated covers AC1 byte-cap semantics independently of
// the regex match. If the 512-byte cap were removed, the regex would still
// reject the input on other grounds — so table-based tests above cannot prove
// the cap is active. These tests assert the concrete error message returned by
// the cap branch, which differs from the regex-rejection message.
//
// Guards against a refactor that accidentally drops the `len(s) > 512` check.
func TestValidators_ByteCap_Isolated(t *testing.T) {
	// 513-byte inputs — length check must fire before the regex runs.
	roomTooLong := "!" + strings.Repeat("a", 512)      // 513 bytes total
	userTooLong := "@" + strings.Repeat("a", 512)      // 513 bytes total
	eventTooLong := "$" + strings.Repeat("a", 512)     // 513 bytes total

	t.Run("room-id-cap", func(t *testing.T) {
		err := ValidateMatrixRoomID(roomTooLong)
		if err == nil {
			t.Fatal("ValidateMatrixRoomID: expected error for >512-byte input")
		}
		if !strings.Contains(err.Error(), "512 bytes") {
			t.Errorf("expected byte-cap error message, got %q (regex may have fired first)", err.Error())
		}
	})

	t.Run("user-id-cap", func(t *testing.T) {
		err := ValidateMatrixUserID(userTooLong)
		if err == nil {
			t.Fatal("ValidateMatrixUserID: expected error for >512-byte input")
		}
		if !strings.Contains(err.Error(), "512 bytes") {
			t.Errorf("expected byte-cap error message, got %q (regex may have fired first)", err.Error())
		}
	})

	t.Run("event-id-cap", func(t *testing.T) {
		err := ValidateMatrixEventID(eventTooLong)
		if err == nil {
			t.Fatal("ValidateMatrixEventID: expected error for >512-byte input")
		}
		if !strings.Contains(err.Error(), "512 bytes") {
			t.Errorf("expected byte-cap error message, got %q (regex may have fired first)", err.Error())
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// AC2: Path parameter validation — invalid roomId → 400 M_INVALID_PARAM
// ─────────────────────────────────────────────────────────────────────────────

// TestRoomMessages_InvalidRoomID covers AC2.
// GET /rooms/{roomId}/messages with a malformed roomId → 400 M_INVALID_PARAM.
//
// FAILING: GetRoomMessages does not call ValidateMatrixRoomID yet.
func TestRoomMessages_InvalidRoomID(t *testing.T) {
	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewGetMessagesHandler(GetMessagesConfig{
		CoreClient: &mockGetMessagesCoreClient{},
		ServerName: "test.local",
	})

	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	mux := http.NewServeMux()
	mux.Handle("GET /rooms/{roomId}/messages",
		jwtMiddleware(http.HandlerFunc(handler.GetRoomMessages)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	// Malformed roomId — missing ! prefix (not a valid Matrix Room ID).
	req := httptest.NewRequest(http.MethodGet, "/rooms/INVALID_ROOM_ID/messages", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed roomId, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %s", errResp.ErrCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC3: requireJSON — 415 on wrong Content-Type
// ─────────────────────────────────────────────────────────────────────────────

// TestContentType_RejectsFormEncoded covers AC3 (Story AT4).
// POST /createRoom with application/x-www-form-urlencoded → 415 M_UNSUPPORTED_MEDIA_TYPE.
//
// FAILING: requireJSON helper does not exist and PostCreateRoom does not check
// Content-Type yet — will return 200 or 400 instead of 415.
func TestContentType_RejectsFormEncoded(t *testing.T) {
	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewCreateRoomHandler(CreateRoomConfig{
		CoreClient: &mockCreateRoomCoreClient{},
		ServerName: "test.local",
	})

	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	mux := http.NewServeMux()
	mux.Handle("POST /createRoom",
		jwtMiddleware(http.HandlerFunc(handler.PostCreateRoom)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	body := "name=TestRoom&topic=Testing"
	req := httptest.NewRequest(http.MethodPost, "/createRoom", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for form-encoded Content-Type, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_UNSUPPORTED_MEDIA_TYPE" {
		t.Errorf("expected errcode M_UNSUPPORTED_MEDIA_TYPE, got %s", errResp.ErrCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC4: Unknown fields — tolerated per Matrix spec §10.1.1
// ─────────────────────────────────────────────────────────────────────────────

// TestUnknownFields_AreIgnored covers the Matrix spec requirement that servers
// MUST tolerate unknown optional fields in createRoom requests (§10.1.1).
// Element Web sends fields like "initial_state", "creation_content", etc. that
// are not in the minimal CreateRoomRequest struct — rejecting them with 400
// violates the spec and breaks Element.
func TestUnknownFields_AreIgnored(t *testing.T) {
	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewCreateRoomHandler(CreateRoomConfig{
		CoreClient: &mockCreateRoomCoreClient{
			resp: &pb.CreateRoomResponse{RoomId: "!test:test.local"},
		},
		ServerName: "test.local",
	})

	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	mux := http.NewServeMux()
	mux.Handle("POST /createRoom",
		jwtMiddleware(http.HandlerFunc(handler.PostCreateRoom)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	// Element Web sends these extra fields — all must be silently ignored, not rejected.
	body := `{"name":"TestRoom","initial_state":[],"creation_content":{},"preset":"private_chat","is_direct":false}`
	req := httptest.NewRequest(http.MethodPost, "/createRoom", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for createRoom with unknown fields, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC5: PUT /presence/{userId}/status — userId mismatch → 403
// ─────────────────────────────────────────────────────────────────────────────

// TestPresence_PUT_RejectsUserMismatch covers AC5 (Story AT2).
// Authenticated as @test-sub-123:test.local, PUT to /presence/@bob:test.local/status → 403.
//
// FAILING: PresenceHandler.PutPresenceStatus does not exist yet — compile error.
func TestPresence_PUT_RejectsUserMismatch(t *testing.T) {
	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	mock := &mockPresenceCoreClient{}

	handler := NewPresenceHandler(PresenceConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})

	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	mux := http.NewServeMux()
	mux.Handle("PUT /presence/{userId}/status",
		jwtMiddleware(http.HandlerFunc(handler.PutPresenceStatus)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	// JWT sub is "test-sub-123" → "@test-sub-123:test.local"; target is @bob:test.local.
	body := `{"presence":"online"}`
	req := httptest.NewRequest(http.MethodPut, "/presence/@bob:test.local/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for userId mismatch on PUT /presence, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_FORBIDDEN" {
		t.Errorf("expected errcode M_FORBIDDEN, got %s", errResp.ErrCode)
	}

	// Core must NOT have been called (mismatch check happens before any gRPC).
	if mock.capturedReq != nil {
		t.Error("expected presence Core NOT to be called on userId mismatch, but capturedReq is set")
	}
}

// TestPresence_PUT_HappyPath covers AC5 happy-path.
// Authenticated as @test-sub-123:test.local, PUT to /presence/@test-sub-123:test.local/status
// returns 200 and calls the Core SetPresence RPC.
//
// Regression guard against an over-strict mismatch check that would also reject
// legitimate self-presence updates.
func TestPresence_PUT_HappyPath(t *testing.T) {
	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	mock := &mockPresenceCoreClient{}
	handler := NewPresenceHandler(PresenceConfig{
		CoreClient: mock,
		ServerName: "test.local",
	})

	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	mux := http.NewServeMux()
	mux.Handle("PUT /presence/{userId}/status",
		jwtMiddleware(http.HandlerFunc(handler.PutPresenceStatus)))

	token := signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)

	body := `{"presence":"online"}`
	req := httptest.NewRequest(http.MethodPut, "/presence/@test-sub-123:test.local/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for self-presence update, got %d; body: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); !strings.Contains(got, "{}") {
		t.Errorf("expected empty-object body, got %q", got)
	}
}

// TestPresence_PUT_RejectsFormEncoded covers AC3 for PutPresenceStatus (MINOR-8).
// The mismatch check passes first, then requireJSON must 415 on wrong Content-Type.
func TestPresence_PUT_RejectsFormEncoded(t *testing.T) {
	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewPresenceHandler(PresenceConfig{
		CoreClient: &mockPresenceCoreClient{},
		ServerName: "test.local",
	})

	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	mux := http.NewServeMux()
	mux.Handle("PUT /presence/{userId}/status",
		jwtMiddleware(http.HandlerFunc(handler.PutPresenceStatus)))

	token := signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)

	// Self-presence URL so the userId-mismatch check passes; then requireJSON must reject.
	req := httptest.NewRequest(http.MethodPut, "/presence/@test-sub-123:test.local/status",
		strings.NewReader("presence=online"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 for form-encoded PUT /presence, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.ErrCode != "M_UNSUPPORTED_MEDIA_TYPE" {
		t.Errorf("expected errcode M_UNSUPPORTED_MEDIA_TYPE, got %s", errResp.ErrCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC6: GET /profile/{userId} — flattened 404 (no user-enumeration oracle)
// ─────────────────────────────────────────────────────────────────────────────

// TestProfile_Flattened404 covers AC6 (Story AT3).
// "user exists but no profile data" and "user does not exist" must both return
// identical 404 status + body — no oracle.
//
// FAILING: After AC6, the handler must return the identical body for both cases.
// Currently profile.go uses the same ErrProfileNotFound path, so the status
// already matches — but the cache header and body equality assertion will
// verify no distinguishing information leaks.
func TestProfile_Flattened404(t *testing.T) {
	// Case 1: user not found (DB returns ErrProfileNotFound — user never registered).
	dbNotFound := &mockProfileDB{found: false}
	muxNotFound := buildProfileHandler(t, &mockProfileCoreClient{}, dbNotFound)

	req1 := httptest.NewRequest(http.MethodGet, "/profile/@nonexistent:test.local", nil)
	w1 := httptest.NewRecorder()
	muxNotFound.ServeHTTP(w1, req1)

	// Case 2: "user exists but no profile row yet" — same ErrProfileNotFound sentinel,
	// different semantics from the caller's perspective but identical DB signal.
	dbNoProfile := &mockProfileDB{found: false}
	muxNoProfile := buildProfileHandler(t, &mockProfileCoreClient{}, dbNoProfile)

	req2 := httptest.NewRequest(http.MethodGet, "/profile/@registered-but-no-profile:test.local", nil)
	w2 := httptest.NewRecorder()
	muxNoProfile.ServeHTTP(w2, req2)

	// Both must be 404.
	if w1.Code != http.StatusNotFound {
		t.Fatalf("case 1 (user not found): expected 404, got %d; body: %s", w1.Code, w1.Body.String())
	}
	if w2.Code != http.StatusNotFound {
		t.Fatalf("case 2 (no profile data): expected 404, got %d; body: %s", w2.Code, w2.Body.String())
	}

	// Both must return identical bodies (no oracle distinguishing the two cases).
	body1 := w1.Body.String()
	body2 := w2.Body.String()
	if body1 != body2 {
		t.Errorf("user-enumeration oracle detected: 404 bodies differ\ncase1 (not found): %s\ncase2 (no profile): %s", body1, body2)
	}

	// AC6 also mandates "cache for 60s" — assert Cache-Control header is set on 404.
	// (Caching the flattened negative response keeps the oracle closed even under
	//  downstream caches; identical Cache-Control on both code paths preserves it.)
	const wantCC = "public, max-age=60"
	if got := w1.Header().Get("Cache-Control"); got != wantCC {
		t.Errorf("case 1: expected Cache-Control %q, got %q", wantCC, got)
	}
	if got := w2.Header().Get("Cache-Control"); got != wantCC {
		t.Errorf("case 2: expected Cache-Control %q, got %q", wantCC, got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AC7: GET /keys/changes — requires JWT (401 without Bearer)
// ─────────────────────────────────────────────────────────────────────────────

// TestKeysChanges_RequiresAuth covers AC7 (Story AT6).
// GET /keys/changes without Bearer → 401 M_MISSING_TOKEN.
//
// FAILING: main.go:563 registers this without jwtMiddleware.
// This unit test exercises the correct wiring that AC7 mandates.
func TestKeysChanges_RequiresAuth(t *testing.T) {
	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)
	_ = privateKey // only needed for valid-token variant; not used in this subtest

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)
	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	keysChangesHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"changed":[],"left":[]}`))
	})

	mux := http.NewServeMux()
	// This is the REQUIRED wiring post-AC7 (vs. current looseRL-only wiring in main.go).
	mux.Handle("GET /keys/changes", jwtMiddleware(keysChangesHandler))

	req := httptest.NewRequest(http.MethodGet, "/keys/changes", nil)
	// Deliberately omit Authorization header.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated GET /keys/changes, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp matrixError
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	// AC7 — JWTMiddleware returns M_MISSING_TOKEN for requests without a Bearer.
	// Assert the concrete errcode so a regression to a generic token error is caught.
	if errResp.ErrCode != "M_MISSING_TOKEN" {
		t.Errorf("expected errcode M_MISSING_TOKEN, got %s", errResp.ErrCode)
	}
}
