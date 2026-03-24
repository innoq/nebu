package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc/connectivity"
)

type fakeCore struct{ state connectivity.State }

func (f fakeCore) State() connectivity.State { return f.state }

func newTestMetrics(t *testing.T, state connectivity.State) (*Metrics, prometheus.Gatherer) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg, fakeCore{state: state})
	return m, reg
}

func TestMetrics_handler_returns_prometheus_format(t *testing.T) {
	_, gatherer := newTestMetrics(t, connectivity.Ready)
	handler := promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
	req := httptest.NewRequest("GET", "/metrics", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type got %q, want text/plain prefix", ct)
	}
}

func TestNebuGrpcStatus_values(t *testing.T) {
	cases := []struct {
		state connectivity.State
		want  float64
	}{
		{connectivity.Ready, 2},
		{connectivity.Idle, 1},
		{connectivity.Connecting, 1},
		{connectivity.TransientFailure, 0},
		{connectivity.Shutdown, 0},
	}
	for _, tc := range cases {
		t.Run(tc.state.String(), func(t *testing.T) {
			_, gatherer := newTestMetrics(t, tc.state)
			mfs, err := gatherer.Gather()
			if err != nil {
				t.Fatalf("Gather: %v", err)
			}
			for _, mf := range mfs {
				if mf.GetName() == "nebu_grpc_status" {
					got := mf.GetMetric()[0].GetGauge().GetValue()
					if got != tc.want {
						t.Errorf("nebu_grpc_status got %v, want %v", got, tc.want)
					}
					return
				}
			}
			t.Error("nebu_grpc_status metric not found")
		})
	}
}

func TestMiddleware_increments_counter_on_200(t *testing.T) {
	m, gatherer := newTestMetrics(t, connectivity.Ready)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	m.Middleware(inner).ServeHTTP(rr, req)

	mfs, _ := gatherer.Gather()
	for _, mf := range mfs {
		if mf.GetName() == "nebu_http_requests_total" {
			labels := mf.GetMetric()[0].GetLabel()
			var method, code string
			for _, l := range labels {
				if l.GetName() == "method" {
					method = l.GetValue()
				}
				if l.GetName() == "status_code" {
					code = l.GetValue()
				}
			}
			if method != "GET" || code != "200" {
				t.Errorf("labels: method=%q status_code=%q; want GET/200", method, code)
			}
			if v := mf.GetMetric()[0].GetCounter().GetValue(); v != 1 {
				t.Errorf("counter value got %v, want 1", v)
			}
			return
		}
	}
	t.Error("nebu_http_requests_total metric not found")
}

func TestMiddleware_captures_503(t *testing.T) {
	m, gatherer := newTestMetrics(t, connectivity.Ready)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	req := httptest.NewRequest("GET", "/ready", nil)
	rr := httptest.NewRecorder()
	m.Middleware(inner).ServeHTTP(rr, req)

	mfs, _ := gatherer.Gather()
	for _, mf := range mfs {
		if mf.GetName() == "nebu_http_requests_total" {
			labels := mf.GetMetric()[0].GetLabel()
			var method, code string
			for _, l := range labels {
				if l.GetName() == "method" {
					method = l.GetValue()
				}
				if l.GetName() == "status_code" {
					code = l.GetValue()
				}
			}
			if method != "GET" || code != "503" {
				t.Errorf("labels: method=%q status_code=%q; want GET/503", method, code)
			}
			return
		}
	}
	t.Error("nebu_http_requests_total metric not found")
}
