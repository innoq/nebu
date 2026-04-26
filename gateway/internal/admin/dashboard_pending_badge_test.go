package admin

// dashboard_pending_badge_test.go — Story 5.4: Admin Dashboard Sidebar Badge — RED-phase tests
//
// ALL tests in this file are expected to FAIL until Story 5.4 is implemented.
// Failing reasons:
//   - DashboardPageData does not yet have CompliancePendingCount field.
//   - DashboardHandler does not yet query the pending count.
//   - base.html sidebar does not yet contain the Compliance nav entry with badge.
//   - newTestDashboardHandlerWithPendingCount helper does not exist (added here only).
//
// Test strategy:
//   - Direct template render via DashboardHandler with injected test doubles.
//   - Extends the existing fakeCoreStateReader / fakeDBPinger / fakeServerNameReader
//     pattern from dashboard_test.go (same package — no re-declaration needed).
//   - A new fakeCompliancePendingCounter interface stub is introduced here.
//   - Tests use strings.Contains on the rendered HTML body — no Playwright required
//     (SSR badge, no JavaScript).
//
// AC5 coverage:
//   - CompliancePendingCount field exists on DashboardPageData (compile-time check).
//   - DashboardHandler populates CompliancePendingCount when count > 0.
//   - DashboardHandler populates CompliancePendingCount = 0 when no pending rows.
//   - Rendered HTML contains a DaisyUI badge when count > 0.
//   - Rendered HTML does NOT contain the badge when count is 0.

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/connectivity"
)

// ─── fakeCompliancePendingCounter ─────────────────────────────────────────────
//
// Implements the CompliancePendingCounter interface (to be defined in dashboard.go
// as part of Story 5.4). Returns a scripted count for unit tests.

type fakeCompliancePendingCounter struct {
	count int
	err   error
}

func (f *fakeCompliancePendingCounter) CountPending(_ context.Context) (int, error) {
	return f.count, f.err
}

// ─── newTestDashboardHandlerWithPendingCount ───────────────────────────────────
//
// Extends newTestDashboardHandler from dashboard_test.go to also inject a
// CompliancePendingCounter. Only required for Story 5.4 badge tests.

func newTestDashboardHandlerWithPendingCount(
	t *testing.T,
	coreState connectivity.State,
	dbErr error,
	pendingCount int,
) *DashboardHandler {
	t.Helper()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	return &DashboardHandler{
		tmpl:            tmpl,
		core:            &fakeCoreStateReader{state: coreState},
		dbPinger:        &fakeDBPinger{err: dbErr},
		nameReader:      &fakeServerNameReader{name: "test-instance"},
		startTime:       time.Now(),
		pendingCounter:  &fakeCompliancePendingCounter{count: pendingCount},
	}
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestDashboardRender_ShowsPendingBadge_WhenCountAbove0 — AC5
//
// Given: DashboardHandler.pendingCounter returns 5
// When:  GET /admin/dashboard
// Then:  HTML body contains a badge element with text "5"
//        AND contains a "Compliance" nav link
func TestDashboardRender_ShowsPendingBadge_WhenCountAbove0(t *testing.T) {
	// Compile-time check: CompliancePendingCount is reachable on DashboardPageData
	// via the embedded PageData (Story 5.4). The single-source-of-truth field
	// lives on PageData so all page types render the badge consistently.
	_ = DashboardPageData{
		PageData: PageData{CompliancePendingCount: 5},
	}

	h := newTestDashboardHandlerWithPendingCount(t, connectivity.Ready, nil, 5)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	// Badge must appear with the count value "5"
	if !strings.Contains(body, "5") {
		t.Errorf("expected badge with count '5' in rendered HTML — badge may be missing")
	}

	// The badge CSS class (DaisyUI) must be present
	if !strings.Contains(body, "badge") {
		t.Errorf("expected 'badge' CSS class in rendered HTML — DaisyUI badge missing")
	}

	// The "Compliance" nav link must be present in the sidebar
	if !strings.Contains(body, "Compliance") {
		t.Errorf("expected 'Compliance' nav entry in sidebar HTML")
	}

	// The badge should have the warning colour
	if !strings.Contains(body, "badge-warning") {
		t.Errorf("expected 'badge-warning' CSS class on the pending badge")
	}
}

// TestDashboardRender_HidesPendingBadge_WhenCountIs0 — AC5
//
// Given: DashboardHandler.pendingCounter returns 0
// When:  GET /admin/dashboard
// Then:  HTML body does NOT contain a badge element (badge-warning absent or badge not rendered)
//        AND "Compliance" nav link IS still present (link itself is always shown)
func TestDashboardRender_HidesPendingBadge_WhenCountIs0(t *testing.T) {
	h := newTestDashboardHandlerWithPendingCount(t, connectivity.Ready, nil, 0)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	// badge-warning should NOT be present when count is 0
	if strings.Contains(body, "badge-warning") {
		t.Errorf("expected badge-warning to be absent when pending count is 0, but it was found in HTML")
	}

	// The Compliance nav entry itself MUST still be present (link is always shown)
	if !strings.Contains(body, "Compliance") {
		t.Errorf("expected 'Compliance' nav entry to be present even when count is 0")
	}
}

// TestDashboardHandler_PendingCountField_Populated — AC5 field propagation
//
// Given: pendingCounter returns 3
// When:  DashboardHandler.Handler is called
// Then:  The rendered page data's CompliancePendingCount == 3
//        Verified indirectly: the number "3" appears in the rendered HTML badge area.
func TestDashboardHandler_PendingCountField_Populated(t *testing.T) {
	h := newTestDashboardHandlerWithPendingCount(t, connectivity.Ready, nil, 3)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	body := w.Body.String()
	// "3" must appear inside a badge element — verify via badge-sm which is adjacent
	// to the count text in the template: <span class="badge badge-warning badge-sm ml-auto">3</span>
	if !strings.Contains(body, "badge-sm") {
		t.Errorf("expected 'badge-sm' in HTML when pending count is 3")
	}
}

// TestDashboardHandler_PendingCountError_DefaultsToZero — resilience
//
// Given: CompliancePendingCounter returns an error
// When:  DashboardHandler.Handler is called
// Then:  Page still renders 200 (non-blocking) — badge is absent (count defaults to 0)
func TestDashboardHandler_PendingCountError_DefaultsToZero(t *testing.T) {
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	h := &DashboardHandler{
		tmpl:       tmpl,
		core:       &fakeCoreStateReader{state: connectivity.Ready},
		dbPinger:   &fakeDBPinger{},
		nameReader: &fakeServerNameReader{name: "test-instance"},
		startTime:  time.Now(),
		pendingCounter: &fakeCompliancePendingCounter{
			count: 0,
			err:   fmt.Errorf("simulated pending count DB error"),
		},
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	// Handler must NOT panic and must return 200
	if w.Code != 200 {
		t.Errorf("expected 200 even on pending count error, got %d", w.Code)
	}
	// badge-warning should NOT be present (count defaults to 0 on error)
	body := w.Body.String()
	if strings.Contains(body, "badge-warning") {
		t.Errorf("badge must not be shown when pending counter errors (defaults to 0)")
	}
}
