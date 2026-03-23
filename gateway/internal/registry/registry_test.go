package registry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Registry unit tests ---

func TestRegistry_RegisterAndList(t *testing.T) {
	reg := New()
	reg.Register("192.168.1.1:9000")

	entries := reg.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Addr != "192.168.1.1:9000" {
		t.Errorf("got addr %q, want %q", entries[0].Addr, "192.168.1.1:9000")
	}
	if entries[0].RegisteredAt.IsZero() {
		t.Error("expected non-zero RegisteredAt")
	}
}

func TestRegistry_ListEmpty(t *testing.T) {
	reg := New()
	entries := reg.List()
	if entries == nil {
		t.Error("expected non-nil slice")
	}
	if len(entries) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(entries))
	}
}

func TestRegistry_RegisterUpdatesEntry(t *testing.T) {
	reg := New()
	reg.Register("10.0.0.1:9000")
	time.Sleep(time.Millisecond) // ensure time progresses
	reg.Register("10.0.0.1:9000")

	entries := reg.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after re-register, got %d", len(entries))
	}
}

func TestRegistry_RegisterMultiple(t *testing.T) {
	reg := New()
	reg.Register("10.0.0.1:9000")
	reg.Register("10.0.0.2:9000")

	entries := reg.List()
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

// --- Handler unit tests ---

func TestHandler_Register(t *testing.T) {
	reg := New()
	h := NewHandler(reg)

	req := httptest.NewRequest("POST", "/internal/nodes/register", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "registered" {
		t.Errorf("got status %q, want %q", resp["status"], "registered")
	}

	entries := reg.List()
	if len(entries) != 1 {
		t.Errorf("expected 1 registered entry, got %d", len(entries))
	}
}

func TestHandler_List_Empty(t *testing.T) {
	reg := New()
	h := NewHandler(reg)

	req := httptest.NewRequest("GET", "/internal/nodes", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}

	var entries []NodeEntry
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty list, got %d entries", len(entries))
	}
}

func TestHandler_List_AfterRegister(t *testing.T) {
	reg := New()
	h := NewHandler(reg)

	// register
	regReq := httptest.NewRequest("POST", "/internal/nodes/register", nil)
	regReq.RemoteAddr = "172.18.0.5:12345"
	regRR := httptest.NewRecorder()
	h.ServeHTTP(regRR, regReq)

	// list
	listReq := httptest.NewRequest("GET", "/internal/nodes", nil)
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, listReq)

	if listRR.Code != http.StatusOK {
		t.Errorf("got %d, want 200", listRR.Code)
	}

	var entries []NodeEntry
	if err := json.NewDecoder(listRR.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Addr != "172.18.0.5" {
		t.Errorf("got addr %q, want %q", entries[0].Addr, "172.18.0.5")
	}
}

func TestHandler_ContentTypeJSON(t *testing.T) {
	reg := New()
	h := NewHandler(reg)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{"POST", "/internal/nodes/register"},
		{"GET", "/internal/nodes"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			ct := rr.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("got Content-Type %q, want %q", ct, "application/json")
			}
		})
	}
}
