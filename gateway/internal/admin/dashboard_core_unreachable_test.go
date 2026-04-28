package admin

// ─── Story 5.29e Bug 3: Admin UI "Core unreachable" after login ──────────────────
//
// ATDD RED PHASE — these tests MUST FAIL until the dashboard behavior is fixed.
//
// Source: tmp/test-findings.md, 2026-04-23.
//   Bug 3: "Nach login kommt 'Core unreachable'" — admin dashboard shows an alarming
//   red error state when gRPC is in connectivity.TransientFailure (common at startup
//   or when compose services haven't fully converged yet).
//
// Root cause: mapCoreState treats connectivity.TransientFailure as "red"/"Unreachable".
// At gateway startup the gRPC connection goes TransientFailure before settling to Ready.
// This causes the admin dashboard to show a red alarm card even when Core is running.
//
// Fix scope (5-29e):
//   1. Reclassify connectivity.TransientFailure → "amber"/"Degraded" (not "red"/"Unreachable").
//      TransientFailure is a transient state; gRPC will retry automatically.
//   2. Only connectivity.Shutdown → "red"/"Unreachable" (Core explicitly stopped).
//   3. The topbar must not show "error" status when Core is in TransientFailure.
//   4. Improve the Core card label for TransientFailure: "Connecting…" or "Retrying"
//      (more informative than "Degraded").
//
// Test strategy:
//   - Extend dashboard_test.go patterns: same newTestDashboardHandler helper.
//   - Assertions on rendered HTML body (status-card--amber vs status-card--red).
//   - Assertions on mapCoreState return values (unit test the mapping function directly).
//   - No hard waits, no DB, no Docker.

import (
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/connectivity"
)

// ─── Unit tests: mapCoreState reclassification ────────────────────────────────────

// TestMapCoreState_TransientFailure_IsAmberNotRed asserts that TransientFailure
// maps to "amber"/"Degraded" (not "red"/"Unreachable").
//
// RED PHASE: currently mapCoreState returns ("red", "Unreachable") for TransientFailure.
// The fix changes this to ("amber", "Degraded") or ("amber", "Connecting…").
func TestMapCoreState_TransientFailure_IsAmberNotRed(t *testing.T) {
	status, _ := mapCoreState(connectivity.TransientFailure)

	if status == "red" {
		t.Errorf("connectivity.TransientFailure must map to 'amber' (transient, gRPC retries automatically), "+
			"but got 'red' — RED PHASE: this failure is expected until the mapCoreState fix is applied. "+
			"Symptom: admin UI shows alarming red 'Core unreachable' at gateway startup.")
	}
	if status != "amber" {
		t.Errorf("connectivity.TransientFailure must map to 'amber', got %q", status)
	}
}

// TestMapCoreState_Shutdown_IsRed asserts that connectivity.Shutdown remains "red".
// Shutdown = Core was explicitly stopped; alarm is warranted.
func TestMapCoreState_Shutdown_IsRed(t *testing.T) {
	status, label := mapCoreState(connectivity.Shutdown)

	if status != "red" {
		t.Errorf("connectivity.Shutdown must map to 'red', got %q", status)
	}
	if label == "" {
		t.Error("label must not be empty for Shutdown state")
	}
}

// TestMapCoreState_Ready_IsGreen asserts that connectivity.Ready remains "green".
// Regression guard: fix must not break the happy path.
func TestMapCoreState_Ready_IsGreenRegression(t *testing.T) {
	status, label := mapCoreState(connectivity.Ready)

	if status != "green" {
		t.Errorf("connectivity.Ready must map to 'green', got %q", status)
	}
	if label != "OK" {
		t.Errorf("connectivity.Ready label must be 'OK', got %q", label)
	}
}

// ─── Integration: dashboard page rendering ────────────────────────────────────────

// TestDashboard_CoreTransientFailure_RendersAmberNotRed asserts that when Core is in
// TransientFailure, the dashboard Core card renders "amber" (not "red").
//
// RED PHASE: currently renders "status-card--red" for TransientFailure.
// Fix: renders "status-card--amber" instead.
func TestDashboard_CoreTransientFailure_RendersAmberNotRed(t *testing.T) {
	h := newTestDashboardHandler(t, connectivity.TransientFailure, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	if w.Code != 200 {
		t.Fatalf("dashboard must return 200 even when Core is in TransientFailure, got %d", w.Code)
	}

	body := w.Body.String()

	// Must NOT render red alarm for TransientFailure.
	if strings.Contains(body, "status-card--red") {
		t.Errorf("dashboard must NOT show 'status-card--red' when Core is in TransientFailure "+
			"(transient states should show amber, not alarm); "+
			"RED PHASE: this failure is expected until mapCoreState is updated.")
	}

	// Must render amber (degraded / connecting) instead.
	if !strings.Contains(body, "status-card--amber") {
		t.Errorf("dashboard must render 'status-card--amber' for Core in TransientFailure state, "+
			"but 'status-card--amber' not found in body")
	}
}

// TestDashboard_CoreTransientFailure_TopbarNotError asserts that TransientFailure
// does NOT cause the topbar to show "error" status.
//
// RED PHASE: currently worstStatus("green", "red", "green") = "red" → topbar = "error".
// Fix: worstStatus("green", "amber", "green") = "amber" → topbar = "warning".
func TestDashboard_CoreTransientFailure_TopbarNotError(t *testing.T) {
	h := newTestDashboardHandler(t, connectivity.TransientFailure, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	body := w.Body.String()

	// Topbar must NOT show "Down" (that is the "error" level label).
	if strings.Contains(body, "Down") {
		t.Errorf("topbar must not show 'Down' label when Core is in TransientFailure (transient); "+
			"RED PHASE: fix mapCoreState to return amber for TransientFailure")
	}
}

// TestDashboard_CoreShutdown_RendersRed asserts that connectivity.Shutdown DOES
// render a red card (regression guard for the intentional alarm case).
func TestDashboard_CoreShutdown_RendersRed(t *testing.T) {
	h := newTestDashboardHandler(t, connectivity.Shutdown, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	body := w.Body.String()

	if !strings.Contains(body, "status-card--red") {
		t.Errorf("dashboard must render 'status-card--red' when Core is Shutdown (intentional stop)")
	}
}

// TestDashboard_CoreReachable_NoRedCard asserts that a healthy Core (Ready state)
// does NOT show any red card — regression guard for the happy path.
func TestDashboard_CoreReachable_NoRedCard(t *testing.T) {
	h := newTestDashboardHandler(t, connectivity.Ready, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	if strings.Contains(body, "status-card--red") {
		t.Errorf("dashboard must NOT show any red card when Core is Ready and DB is OK; body:\n%s", body)
	}
}

// ─── Unit test: mapCoreState comprehensive ────────────────────────────────────────

// TestMapCoreState_AllStates_AfterFix validates the COMPLETE expected mapping after
// the 5-29e fix is applied. This is the single authoritative mapping table.
//
// RED PHASE: the TransientFailure row will fail until the fix is applied.
func TestMapCoreState_AllStates_AfterFix(t *testing.T) {
	cases := []struct {
		state      connectivity.State
		wantStatus string
		wantLabel  string // non-empty string; exact value may vary
	}{
		{connectivity.Ready, "green", "OK"},
		{connectivity.Idle, "amber", "Degraded"},
		{connectivity.Connecting, "amber", "Degraded"},
		// Fix target: TransientFailure must be amber, NOT red.
		// MINOR-4 fix: label pinned to "Connecting…" — implementation has settled on this.
		{connectivity.TransientFailure, "amber", "Connecting…"},
		{connectivity.Shutdown, "red", "Unreachable"},
	}

	for _, tc := range cases {
		gotStatus, gotLabel := mapCoreState(tc.state)

		if gotStatus != tc.wantStatus {
			t.Errorf("mapCoreState(%v): status = %q, want %q "+
				"(RED PHASE for TransientFailure: fix maps it from red → amber)",
				tc.state, gotStatus, tc.wantStatus)
		}

		if tc.wantLabel != "" && gotLabel != tc.wantLabel {
			t.Errorf("mapCoreState(%v): label = %q, want %q", tc.state, gotLabel, tc.wantLabel)
		}

		if gotLabel == "" {
			t.Errorf("mapCoreState(%v): label must not be empty", tc.state)
		}
	}
}
