package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/connectivity"
)

// --- Test doubles ---

// fakeCoreStateReader implements CoreStateReader for testing.
type fakeCoreStateReader struct {
	state connectivity.State
}

func (f *fakeCoreStateReader) State() connectivity.State { return f.state }

// fakeDBPinger implements DBPinger for testing.
type fakeDBPinger struct {
	err error
}

func (f *fakeDBPinger) PingContext(_ context.Context) error { return f.err }

// fakeServerNameReader implements ServerNameReader for testing.
type fakeServerNameReader struct {
	name string
	err  error
}

func (f *fakeServerNameReader) ServerName(_ context.Context) (string, error) {
	return f.name, f.err
}

// newTestDashboardHandler builds a DashboardHandler wired with test doubles.
func newTestDashboardHandler(t *testing.T, coreState connectivity.State, dbErr error) *DashboardHandler {
	t.Helper()
	tmpl, err := NewTemplateHandler()
	if err != nil {
		t.Fatalf("NewTemplateHandler: %v", err)
	}
	return &DashboardHandler{
		tmpl:       tmpl,
		core:       &fakeCoreStateReader{state: coreState},
		dbPinger:   &fakeDBPinger{err: dbErr},
		nameReader: &fakeServerNameReader{name: "test-instance"},
		startTime:  time.Now(),
	}
}

// --- Handler tests ---

// TestDashboardHandler_AllHealthy asserts that when Core=Ready and DB=OK,
// all three status cards render with the "green" CSS class.
func TestDashboardHandler_AllHealthy(t *testing.T) {
	h := newTestDashboardHandler(t, connectivity.Ready, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	count := strings.Count(body, "status-card--green")
	if count < 3 {
		t.Errorf("expected at least 3 occurrences of 'status-card--green', got %d\nbody:\n%s", count, body)
	}
}

// TestDashboardHandler_DBDown asserts that when DB ping fails, the Database card
// shows "red" and the page still renders 200 OK. Gateway card remains green.
func TestDashboardHandler_DBDown(t *testing.T) {
	h := newTestDashboardHandler(t, connectivity.Ready, errors.New("connection refused"))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "status-card--red") {
		t.Error("expected 'status-card--red' in body (Database card should be red)")
	}
	if !strings.Contains(body, "status-card--green") {
		t.Error("expected 'status-card--green' in body (Gateway card should remain green)")
	}
}

// TestDashboardHandler_CoreDegraded asserts that when Core=Idle,
// the Core card shows "amber".
func TestDashboardHandler_CoreDegraded(t *testing.T) {
	h := newTestDashboardHandler(t, connectivity.Idle, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "status-card--amber") {
		t.Error("expected 'status-card--amber' in body (Core card should be amber)")
	}
}

// TestDashboardHandler_CoreDown asserts that when Core=TransientFailure,
// the Core card shows "red".
func TestDashboardHandler_CoreDown(t *testing.T) {
	h := newTestDashboardHandler(t, connectivity.TransientFailure, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "status-card--red") {
		t.Error("expected 'status-card--red' in body (Core card should be red for TransientFailure)")
	}
}

// TestDashboardHandler_ActiveNav asserts that the dashboard nav link has aria-current="page".
func TestDashboardHandler_ActiveNav(t *testing.T) {
	h := newTestDashboardHandler(t, connectivity.Ready, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	body := w.Body.String()
	if !strings.Contains(body, `aria-current="page"`) {
		t.Error("expected aria-current=\"page\" in body (dashboard nav link should be active)")
	}
}

// TestDashboardHandler_ContentType asserts that the response has the correct Content-Type.
func TestDashboardHandler_ContentType(t *testing.T) {
	h := newTestDashboardHandler(t, connectivity.Ready, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/dashboard", nil)
	h.Handler(w, r)

	got := w.Header().Get("Content-Type")
	want := "text/html; charset=utf-8"
	if got != want {
		t.Errorf("Content-Type: got %q, want %q", got, want)
	}
}

// --- Helper unit tests ---

// TestFormatUptime verifies the formatUptime helper for various durations.
func TestFormatUptime(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "<1m"},
		{30 * time.Second, "<1m"},
		{1 * time.Minute, "1m"},
		{90 * time.Second, "1m"},
		{65 * time.Minute, "1h 5m"},
		{24 * time.Hour, "1d"},
		{25*time.Hour + 30*time.Minute, "1d 1h 30m"},
		{3*24*time.Hour + 4*time.Hour + 12*time.Minute, "3d 4h 12m"},
	}
	for _, tc := range cases {
		got := formatUptime(tc.d)
		if got != tc.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// TestWorstStatus verifies the worstStatus helper.
func TestWorstStatus(t *testing.T) {
	cases := []struct {
		statuses []string
		want     string
	}{
		{[]string{"green", "green", "green"}, "green"},
		{[]string{"green", "amber", "green"}, "amber"},
		{[]string{"green", "amber", "red"}, "red"},
		{[]string{"red", "green", "green"}, "red"},
		{[]string{"amber", "amber"}, "amber"},
	}
	for _, tc := range cases {
		got := worstStatus(tc.statuses...)
		if got != tc.want {
			t.Errorf("worstStatus(%v) = %q, want %q", tc.statuses, got, tc.want)
		}
	}
}

// TestMapCoreState verifies the gRPC connectivity → status mapping.
func TestMapCoreState(t *testing.T) {
	cases := []struct {
		state      connectivity.State
		wantStatus string
		wantLabel  string
	}{
		{connectivity.Ready, "green", "OK"},
		{connectivity.Idle, "amber", "Degraded"},
		{connectivity.Connecting, "amber", "Degraded"},
		{connectivity.TransientFailure, "red", "Unreachable"},
		{connectivity.Shutdown, "red", "Unreachable"},
	}
	for _, tc := range cases {
		gotStatus, gotLabel := mapCoreState(tc.state)
		if gotStatus != tc.wantStatus || gotLabel != tc.wantLabel {
			t.Errorf("mapCoreState(%v) = (%q, %q), want (%q, %q)",
				tc.state, gotStatus, gotLabel, tc.wantStatus, tc.wantLabel)
		}
	}
}
