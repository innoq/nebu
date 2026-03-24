package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/connectivity"
)

// fakeCore is a test double for coreState — inline, no external mock lib.
type fakeCore struct {
	state connectivity.State
}

func (f fakeCore) State() connectivity.State { return f.state }

func newTestHandler(core fakeCore, checkDB func(string) error, getMigVersion func(string) (int64, error)) *Handler {
	return &Handler{
		dbURL:         "unused",
		core:          core,
		checkDB:       checkDB,
		getMigVersion: getMigVersion,
	}
}

func okDB(_ string) error           { return nil }
func okMig(_ string) (int64, error) { return 3, nil }

func TestHealth_returns200WithBody(t *testing.T) {
	h := newTestHandler(fakeCore{state: connectivity.Idle}, okDB, okMig)
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	h.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type got %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["status"] != "UP" {
		t.Errorf("status got %q, want UP", body["status"])
	}
	if body["version"] != "0.1.0" {
		t.Errorf("version got %q, want 0.1.0", body["version"])
	}
}

func TestReady_allHealthy(t *testing.T) {
	h := newTestHandler(fakeCore{state: connectivity.Ready}, okDB, okMig)
	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()

	h.Ready(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
	var resp readyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Status != "READY" {
		t.Errorf("status got %q, want READY", resp.Status)
	}
	if resp.Checks.Database.Status != "UP" {
		t.Errorf("database status got %q, want UP", resp.Checks.Database.Status)
	}
	if resp.Checks.CoreGRPC.Status != "UP" {
		t.Errorf("core_grpc status got %q, want UP", resp.Checks.CoreGRPC.Status)
	}
	if resp.Checks.CoreGRPC.NebuStatus != "GRÜN" {
		t.Errorf("nebu_status got %q, want GRÜN", resp.Checks.CoreGRPC.NebuStatus)
	}
	if resp.Checks.Migrations.Status != "UP" {
		t.Errorf("migrations status got %q, want UP", resp.Checks.Migrations.Status)
	}
	if resp.Checks.Migrations.Version != 3 {
		t.Errorf("migrations version got %d, want 3", resp.Checks.Migrations.Version)
	}
}

func TestReady_dbDown_returns503(t *testing.T) {
	failDB := func(_ string) error { return fmt.Errorf("connection refused") }
	h := newTestHandler(fakeCore{state: connectivity.Ready}, failDB, okMig)
	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()

	h.Ready(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", rr.Code)
	}
	var resp readyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Status != "NOT_READY" {
		t.Errorf("status got %q, want NOT_READY", resp.Status)
	}
	if resp.Checks.Database.Status != "DOWN" {
		t.Errorf("database status got %q, want DOWN", resp.Checks.Database.Status)
	}
}

func TestReady_grpcTransientFailure_returns503(t *testing.T) {
	h := newTestHandler(fakeCore{state: connectivity.TransientFailure}, okDB, okMig)
	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()

	h.Ready(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", rr.Code)
	}
	var resp readyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Status != "NOT_READY" {
		t.Errorf("status got %q, want NOT_READY", resp.Status)
	}
	if resp.Checks.CoreGRPC.Status != "DOWN" {
		t.Errorf("core_grpc status got %q, want DOWN", resp.Checks.CoreGRPC.Status)
	}
	if resp.Checks.CoreGRPC.NebuStatus != "ROT" {
		t.Errorf("nebu_status got %q, want ROT", resp.Checks.CoreGRPC.NebuStatus)
	}
}

func TestReady_grpcShutdown_returns503(t *testing.T) {
	h := newTestHandler(fakeCore{state: connectivity.Shutdown}, okDB, okMig)
	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()

	h.Ready(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", rr.Code)
	}
	var resp readyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Checks.CoreGRPC.NebuStatus != "ROT" {
		t.Errorf("nebu_status got %q, want ROT", resp.Checks.CoreGRPC.NebuStatus)
	}
}

func TestReady_grpcIdle_returns503(t *testing.T) {
	h := newTestHandler(fakeCore{state: connectivity.Idle}, okDB, okMig)
	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()

	h.Ready(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", rr.Code)
	}
	var resp readyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Checks.CoreGRPC.NebuStatus != "GELB" {
		t.Errorf("nebu_status got %q, want GELB", resp.Checks.CoreGRPC.NebuStatus)
	}
}

func TestReady_migrationVersionZero_returns503(t *testing.T) {
	zeroMig := func(_ string) (int64, error) { return 0, nil }
	h := newTestHandler(fakeCore{state: connectivity.Ready}, okDB, zeroMig)
	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()

	h.Ready(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", rr.Code)
	}
	var resp readyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Checks.Migrations.Status != "DOWN" {
		t.Errorf("migrations status got %q, want DOWN", resp.Checks.Migrations.Status)
	}
}

func TestReady_contentTypeIsJSON(t *testing.T) {
	h := newTestHandler(fakeCore{state: connectivity.Ready}, okDB, okMig)
	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()

	h.Ready(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type got %q, want application/json", ct)
	}
}
