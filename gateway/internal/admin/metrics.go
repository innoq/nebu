package admin

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/connectivity"
)

// coreStateProvider is the minimal interface needed to read gRPC state.
// *coregrpc.Client satisfies this interface without importing that package.
type coreStateProvider interface {
	State() connectivity.State
}

// Metrics holds all custom Prometheus metrics for the gateway.
type Metrics struct {
	httpRequestsTotal *prometheus.CounterVec
}

// NewMetrics registers nebu_http_requests_total and nebu_grpc_status with reg.
// In production: call with prometheus.DefaultRegisterer.
// In tests: call with prometheus.NewRegistry() to avoid global state pollution.
func NewMetrics(reg prometheus.Registerer, core coreStateProvider) *Metrics {
	m := &Metrics{
		httpRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "nebu_http_requests_total",
			Help: "Total number of completed HTTP requests.",
		}, []string{"method", "status_code"}),
	}
	reg.MustRegister(m.httpRequestsTotal)

	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "nebu_grpc_status",
		Help: "Current gRPC connection status: 2=GRÜN (Ready), 1=GELB (Idle/Connecting), 0=ROT (down).",
	}, func() float64 {
		switch core.State() {
		case connectivity.Ready:
			return 2
		case connectivity.Idle, connectivity.Connecting:
			return 1
		default: // TransientFailure, Shutdown
			return 0
		}
	}))

	return m
}

// Middleware wraps h, recording nebu_http_requests_total after each completed request.
func (m *Metrics) Middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		h.ServeHTTP(rec, r)
		m.httpRequestsTotal.WithLabelValues(r.Method, fmt.Sprintf("%d", rec.code)).Inc()
	})
}

// statusRecorder captures the HTTP status code written by a handler.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.code = code
	sr.ResponseWriter.WriteHeader(code)
}
