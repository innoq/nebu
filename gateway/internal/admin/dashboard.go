package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	"google.golang.org/grpc/connectivity"
)

// CoreStateReader is a minimal consumer-defined interface for reading the gRPC connectivity state.
// *coregrpc.Client satisfies this without requiring the admin package to import the grpc package directly.
type CoreStateReader interface {
	State() connectivity.State
}

// DBPinger is a minimal consumer-defined interface for pinging the database.
// *sql.DB satisfies this directly.
type DBPinger interface {
	PingContext(ctx context.Context) error
}

// ServerNameReader reads the instance server name from persistent storage.
type ServerNameReader interface {
	ServerName(ctx context.Context) (string, error)
}

// CompliancePendingCounter counts pending compliance access requests.
// Implemented by postgresCompliancePendingCounter; replaceable with a test double.
type CompliancePendingCounter interface {
	CountPending(ctx context.Context) (int, error)
}

// postgresServerNameReader reads the server_name from the server_config table.
type postgresServerNameReader struct {
	db *sql.DB
}

// ServerName queries the server_config table for the server_name key.
// Returns "(not configured)" if the row is absent, or an error on unexpected failures.
func (r *postgresServerNameReader) ServerName(ctx context.Context) (string, error) {
	var name string
	err := r.db.QueryRowContext(ctx, "SELECT value FROM server_config WHERE key = 'server_name'").Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "(not configured)", nil
	}
	return name, err
}

// postgresCompliancePendingCounter implements CompliancePendingCounter via a direct DB query.
type postgresCompliancePendingCounter struct {
	db *sql.DB
}

// CountPending returns the number of compliance_requests with status='pending'.
func (c *postgresCompliancePendingCounter) CountPending(ctx context.Context) (int, error) {
	var count int
	err := c.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM compliance_requests WHERE status = 'pending'`,
	).Scan(&count)
	return count, err
}

// DashboardHandler serves the /admin/dashboard SSR page.
type DashboardHandler struct {
	tmpl           *TemplateHandler
	core           CoreStateReader
	dbPinger       DBPinger
	nameReader     ServerNameReader
	startTime      time.Time
	pendingCounter CompliancePendingCounter // Story 5.4: compliance pending-request badge
}

// NewDashboardHandler constructs a DashboardHandler.
// db is used for DB ping, reading server_name, and querying compliance pending count.
// startTime is captured at construction time (= gateway startup) for uptime calculation.
func NewDashboardHandler(tmpl *TemplateHandler, core CoreStateReader, db *sql.DB) *DashboardHandler {
	return &DashboardHandler{
		tmpl:           tmpl,
		core:           core,
		dbPinger:       db,
		nameReader:     &postgresServerNameReader{db: db},
		startTime:      time.Now(),
		pendingCounter: &postgresCompliancePendingCounter{db: db},
	}
}

// Handler renders the dashboard page with SSR system status.
func (h *DashboardHandler) Handler(w http.ResponseWriter, r *http.Request) {
	// --- Core (gRPC) status ---
	coreStatus, coreLabel := mapCoreState(h.core.State())

	// --- Database status ---
	dbStatus, dbLabel := "green", "OK"
	if err := h.dbPinger.PingContext(r.Context()); err != nil {
		dbStatus, dbLabel = "red", "Unreachable"
	}

	// --- Gateway status (always green — if this page loads, gateway is up) ---
	gatewayStatus, gatewayLabel := "green", "OK"

	// --- WorstStatus ---
	worst := worstStatus(gatewayStatus, coreStatus, dbStatus)

	// --- TopbarStatus mapping (DaisyUI semantic color names) ---
	topbarStatus, topbarLabel := mapWorstToTopbar(worst)

	// --- Instance name ---
	instanceName, err := h.nameReader.ServerName(r.Context())
	if err != nil {
		instanceName = "(unknown)"
	}

	// --- Uptime ---
	uptime := formatUptime(time.Since(h.startTime))

	// --- Story 5.4: Compliance pending count (non-blocking: defaults to 0 on error) ---
	var pendingCount int
	if h.pendingCounter != nil {
		if n, cntErr := h.pendingCounter.CountPending(r.Context()); cntErr == nil {
			pendingCount = n
		} else {
			slog.Warn("dashboard: failed to fetch compliance pending count", "err", cntErr)
		}
	}

	pd := newPageData()
	pd.ActiveNav = "dashboard"
	pd.TopbarStatus = topbarStatus
	pd.TopbarLabel = topbarLabel
	pd.CSRFToken = CSRFTokenFromContext(r.Context())
	pd.CompliancePendingCount = pendingCount // Story 5.4 — sidebar badge

	data := DashboardPageData{
		PageData: pd,
		GatewayStatus:      gatewayStatus,
		GatewayStatusLabel: gatewayLabel,
		CoreStatus:         coreStatus,
		CoreStatusLabel:    coreLabel,
		DBStatus:           dbStatus,
		DBStatusLabel:      dbLabel,
		WorstStatus:        worst,
		InstanceName:       instanceName,
		Uptime:             uptime,
		GoVersion:          runtime.Version(),
	}

	h.tmpl.render(w, "dashboard", data)
}

// mapCoreState translates a gRPC connectivity.State into a status string and label.
//
// Story 5-29e fix: TransientFailure reclassified from "red"/"Unreachable" to
// "amber"/"Connecting…". TransientFailure is a transient gRPC state — the client
// retries automatically. Only Shutdown (explicit stop) warrants a red alarm.
//
// Mapping table (5-29e authoritative):
//   - Ready          → "green", "OK"
//   - Idle           → "amber", "Degraded"
//   - Connecting     → "amber", "Degraded"
//   - TransientFailure → "amber", "Connecting…"  (was "red" — Bug 3 fix)
//   - Shutdown       → "red",   "Unreachable"
func mapCoreState(s connectivity.State) (status, label string) {
	switch s {
	case connectivity.Ready:
		return "green", "OK"
	case connectivity.Idle, connectivity.Connecting:
		return "amber", "Degraded"
	case connectivity.TransientFailure:
		return "amber", "Connecting…"
	default: // Shutdown
		return "red", "Unreachable"
	}
}

// worstStatus returns the worst status from a list of status strings.
// Priority order: "red" > "amber" > "green".
func worstStatus(statuses ...string) string {
	for _, s := range statuses {
		if s == "red" {
			return "red"
		}
	}
	for _, s := range statuses {
		if s == "amber" {
			return "amber"
		}
	}
	return "green"
}

// mapWorstToTopbar converts an internal worst status to DaisyUI topbar values.
func mapWorstToTopbar(worst string) (topbarStatus, topbarLabel string) {
	switch worst {
	case "red":
		return "error", "Down"
	case "amber":
		return "warning", "Degraded"
	default:
		return "success", "OK"
	}
}

// formatUptime formats a duration as a human-readable uptime string.
// Examples: "3d 4h 12m", "4h 12m", "12m", "<1m".
// Zero-value units are omitted except the minimum "<1m" for very short uptimes.
func formatUptime(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	var result string
	if days > 0 {
		result += fmt.Sprintf("%dd ", days)
	}
	if hours > 0 {
		result += fmt.Sprintf("%dh ", hours)
	}
	if minutes > 0 {
		result += fmt.Sprintf("%dm", minutes)
	}

	// Trim trailing space if only days+hours were non-zero with no minutes
	if len(result) > 0 && result[len(result)-1] == ' ' {
		result = result[:len(result)-1]
	}

	return result
}
