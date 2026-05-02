package matrix

// ─── Story 7-26: Device Management — GET/PUT/DELETE /devices + POST /delete_devices ──
//
// Tests written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file MUST FAIL until devices.go is created and routes are registered.
//
// Acceptance Criteria covered:
//   AC1 — GET /devices → 200 with {"devices":[...]} listing all sessions for the authenticated user
//   AC2 — GET /devices/{deviceId} → 200 with device object; unknown → 404 M_NOT_FOUND
//   AC3 — PUT /devices/{deviceId} → 200 {} + updates display_name; subsequent GET reflects change
//   AC4 — DELETE /devices/{deviceId} with no auth body → 401 UIA challenge (flows, session, params)
//   AC5 — DELETE own current device → 403 M_FORBIDDEN
//   AC6 — device_display_name migration exists (tested by migration harness)
//   AC7 — UIA: completed SSO session allows DELETE → 200 {}
//   IDOR — user A cannot read/modify/delete user B's device
//
// Design:
//   - DevicesHandler wraps all five endpoints; constructor takes DevicesConfig.
//   - DevicesDB is the consumer-defined interface for sessions table access.
//   - mockDevicesDB implements DevicesDB in-memory.
//   - Tests cover happy paths + IDOR + ownership + UIA challenge flow.
//
// NOTE: DevicesHandler, DevicesConfig, NewDevicesHandler, DevicesDB,
// ListDevicesHandler, GetDeviceHandler, PutDeviceHandler, DeleteDeviceHandler,
// DeleteDevicesHandler are declared in gateway/internal/matrix/devices.go —
// which does NOT exist yet. Every test in this file MUST fail with a compilation
// error until devices.go is created.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Mock DevicesDB ───────────────────────────────────────────────────────────

// DeviceRecord mirrors the DB row for a device/session.
// Mirrors the struct declared in devices.go.
type mockDevice struct {
	DeviceID      string
	UserID        string
	DisplayName   *string // nullable
	LastSeenTS    int64   // Unix ms
	LastSeenIP    *string // nullable
}

// mockDevicesDB implements DevicesDB (defined in devices.go).
// Stores device records in an in-memory map keyed by deviceId.
type mockDevicesDB struct {
	mu      sync.Mutex
	devices map[string]mockDevice // key: deviceId

	// Error injection
	listErr   error
	getErr    error
	putErr    error
	deleteErr error
}

func newMockDevicesDB() *mockDevicesDB {
	return &mockDevicesDB{devices: make(map[string]mockDevice)}
}

func (m *mockDevicesDB) addDevice(d mockDevice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[d.DeviceID] = d
}

// ListDevices implements DevicesDB.
func (m *mockDevicesDB) ListDevices(ctx context.Context, userID string) ([]Device, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []Device
	for _, d := range m.devices {
		if d.UserID == userID {
			result = append(result, Device{
				DeviceID:    d.DeviceID,
				DisplayName: d.DisplayName,
				LastSeenTS:  d.LastSeenTS,
				LastSeenIP:  d.LastSeenIP,
			})
		}
	}
	return result, nil
}

// GetDevice implements DevicesDB.
func (m *mockDevicesDB) GetDevice(ctx context.Context, userID, deviceID string) (*Device, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[deviceID]
	if !ok || d.UserID != userID {
		return nil, ErrDeviceNotFound
	}
	return &Device{
		DeviceID:    d.DeviceID,
		DisplayName: d.DisplayName,
		LastSeenTS:  d.LastSeenTS,
		LastSeenIP:  d.LastSeenIP,
	}, nil
}

// UpdateDeviceDisplayName implements DevicesDB.
func (m *mockDevicesDB) UpdateDeviceDisplayName(ctx context.Context, userID, deviceID string, displayName *string) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[deviceID]
	if !ok || d.UserID != userID {
		return ErrDeviceNotFound
	}
	d.DisplayName = displayName
	m.devices[deviceID] = d
	return nil
}

// DeleteDevice implements DevicesDB.
func (m *mockDevicesDB) DeleteDevice(ctx context.Context, userID, deviceID string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[deviceID]
	if !ok || d.UserID != userID {
		return ErrDeviceNotFound
	}
	delete(m.devices, deviceID)
	return nil
}

// DeleteDevices implements DevicesDB — atomically deletes multiple devices.
func (m *mockDevicesDB) DeleteDevices(ctx context.Context, userID string, deviceIDs []string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range deviceIDs {
		d, ok := m.devices[id]
		if ok && d.UserID == userID {
			delete(m.devices, id)
		}
		// Silently ignore devices not belonging to user (AC5).
	}
	return nil
}

// ─── Helper ──────────────────────────────────────────────────────────────────

// buildAuthedDevicesHandler wires JWTMiddleware → DevicesHandler and registers all
// five routes on a mux so PathValue resolves correctly.
//
// The JWT subject is "test-sub-123" → user_id "@test-sub-123:test.local".
// Pass extraClaims to inject additional JWT claims (e.g. "did" for device_id).
func buildAuthedDevicesHandler(t *testing.T, db *mockDevicesDB) (http.Handler, func(extraClaims map[string]any) string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)

	handler := NewDevicesHandler(DevicesConfig{
		DB:         db,
		ServerName: "test.local",
	})

	jwt := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, "test.local")

	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/client/v3/devices",
		jwt(http.HandlerFunc(handler.ListDevices)))
	mux.Handle("GET /_matrix/client/v3/devices/{deviceId}",
		jwt(http.HandlerFunc(handler.GetDevice)))
	mux.Handle("PUT /_matrix/client/v3/devices/{deviceId}",
		jwt(http.HandlerFunc(handler.PutDevice)))
	mux.Handle("DELETE /_matrix/client/v3/devices/{deviceId}",
		jwt(http.HandlerFunc(handler.DeleteDevice)))
	mux.Handle("POST /_matrix/client/v3/delete_devices",
		jwt(http.HandlerFunc(handler.DeleteDevices)))

	makeToken := func(extraClaims map[string]any) string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), extraClaims)
	}

	return mux, makeToken
}

// authedDevicesUserID is the Matrix user_id produced by the mock JWT helper.
const authedDevicesUserID = "@test-sub-123:test.local"

// ─── AC1: GET /devices returns all devices for user ──────────────────────────

func TestListDevices_TwoDevices_ReturnsBoth(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "DEVICE_A", UserID: authedDevicesUserID, LastSeenTS: 1000})
	db.addDevice(mockDevice{DeviceID: "DEVICE_B", UserID: authedDevicesUserID, LastSeenTS: 2000})
	// Add a device for a different user — should not appear.
	db.addDevice(mockDevice{DeviceID: "DEVICE_OTHER", UserID: "@other:test.local", LastSeenTS: 3000})

	mux, makeToken := buildAuthedDevicesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/devices", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body struct {
		Devices []Device `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if len(body.Devices) != 2 {
		t.Errorf("expected 2 devices, got %d; devices: %v", len(body.Devices), body.Devices)
	}
	for _, d := range body.Devices {
		if d.DeviceID != "DEVICE_A" && d.DeviceID != "DEVICE_B" {
			t.Errorf("unexpected device_id %q in response", d.DeviceID)
		}
		if d.DeviceID == "DEVICE_OTHER" {
			t.Errorf("device from other user must not appear in response")
		}
	}
}

func TestListDevices_NoDevices_ReturnsEmptyArray(t *testing.T) {
	db := newMockDevicesDB()
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/devices", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body struct {
		Devices []Device `json:"devices"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	// Must return empty array, never null.
	if body.Devices == nil {
		t.Error("'devices' must not be null; expected empty array []")
	}
}

func TestListDevices_Unauthenticated_Returns401(t *testing.T) {
	db := newMockDevicesDB()
	mux, _ := buildAuthedDevicesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/devices", nil)
	// No Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── AC2: GET /devices/{deviceId} ────────────────────────────────────────────

func TestGetDevice_KnownDevice_Returns200(t *testing.T) {
	db := newMockDevicesDB()
	displayName := "My Phone"
	db.addDevice(mockDevice{
		DeviceID:    "DEVICE_A",
		UserID:      authedDevicesUserID,
		DisplayName: &displayName,
		LastSeenTS:  1000,
	})
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/devices/DEVICE_A", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var device Device
	if err := json.Unmarshal(w.Body.Bytes(), &device); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if device.DeviceID != "DEVICE_A" {
		t.Errorf("expected device_id DEVICE_A, got %q", device.DeviceID)
	}
	if device.DisplayName == nil || *device.DisplayName != "My Phone" {
		t.Errorf("expected display_name 'My Phone', got %v", device.DisplayName)
	}
}

func TestGetDevice_UnknownDevice_Returns404(t *testing.T) {
	db := newMockDevicesDB()
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/devices/NONEXISTENT", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_NOT_FOUND") {
		t.Errorf("expected M_NOT_FOUND in body, got: %s", w.Body.String())
	}
}

// AC2 security: IDOR — user A cannot GET user B's device
func TestGetDevice_IDORProtection_Returns404(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "VICTIM_DEVICE", UserID: "@other:test.local", LastSeenTS: 1000})
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/devices/VICTIM_DEVICE", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("IDOR: expected 404 for other user's device, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── AC3: PUT /devices/{deviceId} updates display_name ───────────────────────

func TestPutDevice_UpdatesDisplayName_GetReflects(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "DEVICE_A", UserID: authedDevicesUserID, LastSeenTS: 1000})
	mux, makeToken := buildAuthedDevicesHandler(t, db)
	token := makeToken(nil)

	putBody := `{"display_name":"Work Laptop"}`
	putReq := httptest.NewRequest(http.MethodPut, "/_matrix/client/v3/devices/DEVICE_A",
		bytes.NewBufferString(putBody))
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/json")

	putW := httptest.NewRecorder()
	mux.ServeHTTP(putW, putReq)

	if putW.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d; body: %s", putW.Code, putW.Body.String())
	}
	if strings.TrimSpace(putW.Body.String()) != "{}" {
		t.Errorf("PUT: expected body '{}', got %q", putW.Body.String())
	}

	// GET — should reflect updated display_name
	getReq := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/devices/DEVICE_A", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)

	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}
	if !strings.Contains(getW.Body.String(), "Work Laptop") {
		t.Errorf("GET: expected display_name 'Work Laptop' in body, got: %s", getW.Body.String())
	}
}

func TestPutDevice_UnknownDevice_Returns404(t *testing.T) {
	db := newMockDevicesDB()
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	putBody := `{"display_name":"Ghost"}`
	req := httptest.NewRequest(http.MethodPut, "/_matrix/client/v3/devices/NONEXISTENT",
		bytes.NewBufferString(putBody))
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_NOT_FOUND") {
		t.Errorf("expected M_NOT_FOUND in body, got: %s", w.Body.String())
	}
}

func TestPutDevice_BadJSON_Returns400(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "DEVICE_A", UserID: authedDevicesUserID, LastSeenTS: 1000})
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	req := httptest.NewRequest(http.MethodPut, "/_matrix/client/v3/devices/DEVICE_A",
		bytes.NewBufferString("not json"))
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_BAD_JSON") {
		t.Errorf("expected M_BAD_JSON in body, got: %s", w.Body.String())
	}
}

// AC3 security: IDOR — user A cannot PUT user B's device
func TestPutDevice_IDORProtection_Returns404(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "VICTIM_DEVICE", UserID: "@other:test.local", LastSeenTS: 1000})
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	req := httptest.NewRequest(http.MethodPut, "/_matrix/client/v3/devices/VICTIM_DEVICE",
		bytes.NewBufferString(`{"display_name":"Hacked"}`))
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("IDOR: expected 404 for other user's device, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── AC4: DELETE /devices/{deviceId} — UIA challenge ─────────────────────────

func TestDeleteDevice_NoAuthBody_Returns401WithChallenge(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "DEVICE_B", UserID: authedDevicesUserID, LastSeenTS: 1000})
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	req := httptest.NewRequest(http.MethodDelete, "/_matrix/client/v3/devices/DEVICE_B", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "flows") {
		t.Errorf("expected 'flows' in UIA challenge body, got: %s", body)
	}
	if !strings.Contains(body, "session") {
		t.Errorf("expected 'session' in UIA challenge body, got: %s", body)
	}
	if !strings.Contains(body, "params") {
		t.Errorf("expected 'params' in UIA challenge body, got: %s", body)
	}
	// Flows must contain m.login.sso
	if !strings.Contains(body, "m.login.sso") {
		t.Errorf("expected 'm.login.sso' in flows, got: %s", body)
	}
}

// ─── AC5: DELETE own current device is forbidden ─────────────────────────────

func TestDeleteDevice_OwnCurrentDevice_Returns403(t *testing.T) {
	// The handler detects the current device via the "did" claim in the JWT.
	// We inject did=CALLING_DEVICE into the JWT so the handler knows which
	// device is "current". Attempting to DELETE that same device must be rejected.
	const callingDeviceID = "CALLING_DEVICE"
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: callingDeviceID, UserID: authedDevicesUserID, LastSeenTS: 1000})

	mux, makeToken := buildAuthedDevicesHandler(t, db)

	// Pre-approve a UIA session so UIA doesn't block (we test AC5, not AC4).
	sessionID := "test-uia-session-forbidden"
	approveUIASession(sessionID, authedDevicesUserID)

	// Inject "did" claim so the handler knows CALLING_DEVICE is the current device.
	token := makeToken(map[string]any{"did": callingDeviceID})

	body := bytes.NewBufferString(`{"auth":{"type":"m.login.sso","session":"` + sessionID + `"}}`)
	req := httptest.NewRequest(http.MethodDelete, "/_matrix/client/v3/devices/"+callingDeviceID, body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 M_FORBIDDEN for own current device, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_FORBIDDEN") {
		t.Errorf("expected M_FORBIDDEN in body, got: %s", w.Body.String())
	}
}

// ─── AC7: DELETE with valid completed UIA session → 200 ──────────────────────

func TestDeleteDevice_CompletedUIASession_Deletes(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "DEVICE_TO_DELETE", UserID: authedDevicesUserID, LastSeenTS: 1000})
	db.addDevice(mockDevice{DeviceID: "OTHER_DEVICE", UserID: authedDevicesUserID, LastSeenTS: 2000})
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	// Pre-approve a UIA session.
	sessionID := "test-uia-session-ok"
	approveUIASession(sessionID, authedDevicesUserID)

	deleteBody := bytes.NewBufferString(`{"auth":{"type":"m.login.sso","session":"` + sessionID + `"}}`)
	req := httptest.NewRequest(http.MethodDelete, "/_matrix/client/v3/devices/DEVICE_TO_DELETE", deleteBody)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) != "{}" {
		t.Errorf("expected body '{}', got %q", w.Body.String())
	}

	// Verify the device was actually deleted.
	if _, err := db.GetDevice(context.Background(), authedDevicesUserID, "DEVICE_TO_DELETE"); err == nil {
		t.Errorf("expected DEVICE_TO_DELETE to be deleted, but it still exists")
	}
}

// ─── AC6 (bulk delete): POST /delete_devices atomically deletes listed devices ─

func TestDeleteDevices_BulkDelete_DeletesListedDevices(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "SESSION_A", UserID: authedDevicesUserID, LastSeenTS: 1000})
	db.addDevice(mockDevice{DeviceID: "SESSION_B", UserID: authedDevicesUserID, LastSeenTS: 2000})
	db.addDevice(mockDevice{DeviceID: "SESSION_C", UserID: authedDevicesUserID, LastSeenTS: 3000})
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	// Pre-approve a UIA session.
	sessionID := "test-bulk-delete-session"
	approveUIASession(sessionID, authedDevicesUserID)

	body := bytes.NewBufferString(`{"devices":["SESSION_B","SESSION_C"],"auth":{"type":"m.login.sso","session":"` + sessionID + `"}}`)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/delete_devices", body)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// SESSION_A must still exist.
	if _, err := db.GetDevice(context.Background(), authedDevicesUserID, "SESSION_A"); err != nil {
		t.Errorf("SESSION_A must still exist after bulk delete of B and C, got error: %v", err)
	}
	// SESSION_B and SESSION_C must be gone.
	if _, err := db.GetDevice(context.Background(), authedDevicesUserID, "SESSION_B"); err == nil {
		t.Errorf("SESSION_B must be deleted but still exists")
	}
	if _, err := db.GetDevice(context.Background(), authedDevicesUserID, "SESSION_C"); err == nil {
		t.Errorf("SESSION_C must be deleted but still exists")
	}
}

func TestDeleteDevices_BulkDelete_IgnoresUnknownDevices(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "SESSION_A", UserID: authedDevicesUserID, LastSeenTS: 1000})
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	sessionID := "test-bulk-delete-unknown"
	approveUIASession(sessionID, authedDevicesUserID)

	body := bytes.NewBufferString(`{"devices":["SESSION_A","DOES_NOT_EXIST"],"auth":{"type":"m.login.sso","session":"` + sessionID + `"}}`)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/delete_devices", body)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Must succeed (200) even though DOES_NOT_EXIST doesn't exist (AC5: silently ignores).
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestDeleteDevices_NoAuthBody_Returns401Challenge(t *testing.T) {
	db := newMockDevicesDB()
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	body := bytes.NewBufferString(`{"devices":["SESSION_A"]}`)
	req := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/delete_devices", body)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 UIA challenge, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "flows") {
		t.Errorf("expected 'flows' in body, got: %s", w.Body.String())
	}
}

// ─── UIA session expiry ───────────────────────────────────────────────────────

func TestDeleteDevice_ExpiredUIASession_Returns401(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "DEVICE_X", UserID: authedDevicesUserID, LastSeenTS: 1000})
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	// Use a random UUID that was never approved → should trigger UIA challenge.
	body := bytes.NewBufferString(`{"auth":{"type":"m.login.sso","session":"never-approved-session"}}`)
	req := httptest.NewRequest(http.MethodDelete, "/_matrix/client/v3/devices/DEVICE_X", body)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown UIA session, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── UIA session tied to user ─────────────────────────────────────────────────

func TestDeleteDevice_UIASessionFromDifferentUser_Returns401(t *testing.T) {
	db := newMockDevicesDB()
	db.addDevice(mockDevice{DeviceID: "DEVICE_X", UserID: authedDevicesUserID, LastSeenTS: 1000})
	mux, makeToken := buildAuthedDevicesHandler(t, db)

	// Approve a UIA session for a DIFFERENT user.
	sessionID := "cross-user-uia-session"
	approveUIASession(sessionID, "@evil:test.local")

	body := bytes.NewBufferString(`{"auth":{"type":"m.login.sso","session":"` + sessionID + `"}}`)
	req := httptest.NewRequest(http.MethodDelete, "/_matrix/client/v3/devices/DEVICE_X", body)
	req.Header.Set("Authorization", "Bearer "+makeToken(nil))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Must reject: UIA session belongs to @evil, not @test-sub-123.
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("cross-user UIA: expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
}
