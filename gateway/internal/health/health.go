package health

import (
	"encoding/json"
	"net/http"

	"google.golang.org/grpc/connectivity"

	"github.com/nebu/nebu/internal/db"
)

const gatewayVersion = "0.1.0"

// coreState is the minimal interface the health handler needs from the gRPC client.
type coreState interface {
	State() connectivity.State
}

type Handler struct {
	dbURL         string
	core          coreState
	checkDB       func(string) error
	getMigVersion func(string) (int64, error)
}

// NewHandler creates a Handler with production DB check functions.
func NewHandler(dbURL string, core coreState) *Handler {
	return &Handler{
		dbURL:         dbURL,
		core:          core,
		checkDB:       db.CheckDB,
		getMigVersion: db.GetMigrationVersion,
	}
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type dbCheck struct {
	Status string `json:"status"`
}

type coreGRPCCheck struct {
	Status     string `json:"status"`
	NebuStatus string `json:"nebu_status"`
}

type migrationsCheck struct {
	Status  string `json:"status"`
	Version int64  `json:"version,omitempty"`
}

type readyChecks struct {
	Database   dbCheck         `json:"database"`
	CoreGRPC   coreGRPCCheck   `json:"core_grpc"`
	Migrations migrationsCheck `json:"migrations"`
}

type readyResponse struct {
	Status string      `json:"status"`
	Checks readyChecks `json:"checks"`
}

// Health is a liveness probe — always returns 200 if the process is running.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "UP", Version: gatewayVersion})
}

// Ready checks all dependencies and returns READY (200) or NOT_READY (503).
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	checks, allReady := h.runChecks()

	resp := readyResponse{Status: "READY", Checks: checks}
	if !allReady {
		resp.Status = "NOT_READY"
	}

	w.Header().Set("Content-Type", "application/json")
	if !allReady {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) runChecks() (readyChecks, bool) {
	var checks readyChecks
	allReady := true

	// DB check
	if err := h.checkDB(h.dbURL); err != nil {
		checks.Database = dbCheck{Status: "DOWN"}
		allReady = false
	} else {
		checks.Database = dbCheck{Status: "UP"}
	}

	// Migration version check
	version, err := h.getMigVersion(h.dbURL)
	if err != nil || version == 0 {
		checks.Migrations = migrationsCheck{Status: "DOWN"}
		allReady = false
	} else {
		checks.Migrations = migrationsCheck{Status: "UP", Version: version}
	}

	// gRPC state check
	state := h.core.State()
	switch state {
	case connectivity.Ready:
		checks.CoreGRPC = coreGRPCCheck{Status: "UP", NebuStatus: "GRÜN"}
	case connectivity.Idle, connectivity.Connecting:
		checks.CoreGRPC = coreGRPCCheck{Status: "UP", NebuStatus: "GELB"}
		allReady = false
	default: // TransientFailure, Shutdown
		checks.CoreGRPC = coreGRPCCheck{Status: "DOWN", NebuStatus: "ROT"}
		allReady = false
	}

	return checks, allReady
}
