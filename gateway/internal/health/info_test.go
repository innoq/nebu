package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestInfoHandler_WithBuildVars verifies AC1: the handler returns 200 JSON with all four
// build-metadata fields populated when constructed with explicit values.
func TestInfoHandler_WithBuildVars(t *testing.T) {
	handler := NewInfoHandler("gateway", "0.1.0", "abc1234", "2026-05-11T10:00:00Z")

	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status got %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type got %q, want application/json", ct)
	}

	var body struct {
		Component string `json:"component"`
		Version   string `json:"version"`
		GitCommit string `json:"git_commit"`
		BuildTime string `json:"build_time"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body.Component != "gateway" {
		t.Errorf("component got %q, want %q", body.Component, "gateway")
	}
	if body.Version != "0.1.0" {
		t.Errorf("version got %q, want %q", body.Version, "0.1.0")
	}
	if body.GitCommit != "abc1234" {
		t.Errorf("git_commit got %q, want %q", body.GitCommit, "abc1234")
	}
	if body.BuildTime != "2026-05-11T10:00:00Z" {
		t.Errorf("build_time got %q, want %q", body.BuildTime, "2026-05-11T10:00:00Z")
	}
}

// TestInfoHandler_UnknownFallbacks verifies AC3: when built without ldflags every field
// carries the sentinel value "unknown" — no empty strings, no panic.
func TestInfoHandler_UnknownFallbacks(t *testing.T) {
	handler := NewInfoHandler("gateway", "unknown", "unknown", "unknown")

	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status got %d, want 200", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}

	for _, field := range []string{"component", "version", "git_commit", "build_time"} {
		v, ok := body[field]
		if !ok {
			t.Errorf("field %q is missing from response", field)
			continue
		}
		if v == "" {
			t.Errorf("field %q must not be an empty string — want %q", field, "unknown")
		}
	}

	// component is always "gateway" regardless of ldflags
	if body["component"] != "gateway" {
		t.Errorf("component got %q, want %q", body["component"], "gateway")
	}
	if body["version"] != "unknown" {
		t.Errorf("version got %q, want %q", body["version"], "unknown")
	}
	if body["git_commit"] != "unknown" {
		t.Errorf("git_commit got %q, want %q", body["git_commit"], "unknown")
	}
	if body["build_time"] != "unknown" {
		t.Errorf("build_time got %q, want %q", body["build_time"], "unknown")
	}
}

// TestInfoHandler_NoAuthRequired verifies AC4: the /info endpoint returns 200 even when
// no Authorization header is present — it must be on the public mux, not the auth mux.
func TestInfoHandler_NoAuthRequired(t *testing.T) {
	handler := NewInfoHandler("gateway", "0.1.0", "abc1234", "2026-05-11T10:00:00Z")

	// Deliberately omit the Authorization header.
	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code == http.StatusUnauthorized {
		t.Errorf("got 401 — /info must not require authentication")
	}
	if rr.Code == http.StatusForbidden {
		t.Errorf("got 403 — /info must not require authentication")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status got %d, want 200", rr.Code)
	}
}
