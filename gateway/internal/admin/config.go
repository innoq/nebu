package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// AdminConfigClient is a minimal consumer-defined interface for the admin server-config gRPC RPCs.
// *grpc.Client already satisfies this interface — no new wrapper methods needed in client.go.
// In tests, pass nil (variadic constructor) — handler falls back to stub data when core is nil.
type AdminConfigClient interface {
	GetServerConfig(ctx context.Context, req *pb.GetServerConfigRequest) (*pb.GetServerConfigResponse, error)
	UpdateServerConfig(ctx context.Context, req *pb.UpdateServerConfigRequest) (*pb.UpdateServerConfigResponse, error)
}

// ConfigHandler serves the Server Configuration page (Story 7.10).
// Story 9.4: backed by real gRPC calls to the Elixir core when core is non-nil.
// Falls back to stub data when core is nil (unit-test path).
type ConfigHandler struct {
	tmpl *TemplateHandler
	core AdminConfigClient
}

// NewConfigHandler creates a ConfigHandler with the given template handler and optional gRPC client.
// Pass nil (or omit) for core to use stub data (unit-test path; stub fallback preserved for
// backward compatibility with existing unit tests).
func NewConfigHandler(tmpl *TemplateHandler, core ...AdminConfigClient) *ConfigHandler {
	var c AdminConfigClient
	if len(core) > 0 {
		c = core[0]
	}
	return &ConfigHandler{tmpl: tmpl, core: c}
}

// protoToStubConfig maps a ServerConfigProto to StubConfig for template rendering.
// Template compatibility: retaining StubConfig avoids a cross-file refactor.
// AllowRegistration: not in proto (no corresponding server config field in the current data model).
// It is preserved in StubConfig for backward compatibility; the checkbox remains UI-only.
// NOTE: when core is nil, the stub default (AllowRegistration: true) is used.
func protoToStubConfig(p *pb.ServerConfigProto) StubConfig {
	return StubConfig{
		InstanceName:    p.GetInstanceName(),
		MaxRoomsPerUser: int(p.GetRoomDefaultMaxMembers()),
		RetentionDays:   int(p.GetAuditLogRetentionDays()),
		// AllowRegistration has no proto equivalent; keep UI-only state from stub default.
		AllowRegistration: stubConfig.AllowRegistration,
	}
}

// Handler serves GET /admin/config.
// When core is set: fetches config from gRPC GetServerConfig.
// When core is nil: falls back to stub data (unit-test path).
// Renders config.html with ConfigPageData. Reads the ?flash= query param.
func (h *ConfigHandler) Handler(w http.ResponseWriter, r *http.Request) {
	var flash AlertBannerData
	if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != "" {
		flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
	}

	cfg := stubConfig
	if h.core != nil {
		resp, err := h.core.GetServerConfig(r.Context(), &pb.GetServerConfigRequest{})
		if err != nil {
			slog.Error("admin: GetServerConfig gRPC error", "err", err)
			// Render with stub fallback + error flash — do not panic.
			flash = AlertBannerData{Severity: "error", Message: "Failed to load server config. Showing cached defaults.", Dismissible: true}
		} else if resp.GetConfig() != nil {
			cfg = protoToStubConfig(resp.GetConfig())
		}
	}

	configPD := newPageData()
	configPD.ActiveNav = "config"
	configPD.CSRFToken = CSRFTokenFromContext(r.Context())
	data := ConfigPageData{
		PageData: configPD,
		Config:   cfg,
		Flash:    flash,
	}
	h.tmpl.render(w, "config", data)
}

// UpdateConfigHandler handles POST /admin/config.
// When core is set: validates form fields and calls gRPC UpdateServerConfig (upsert semantics).
// When core is nil: falls back to stub mutation (unit-test path).
// Redirects with PRG on success.
func (h *ConfigHandler) UpdateConfigHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	instanceName := strings.TrimSpace(r.FormValue("instance_name"))
	if instanceName == "" {
		http.Error(w, "instance_name must not be empty", http.StatusBadRequest)
		return
	}

	maxRooms, err := strconv.Atoi(r.FormValue("max_rooms_per_user"))
	if err != nil || maxRooms < 1 || maxRooms > 100 {
		http.Error(w, "max_rooms_per_user must be between 1 and 100", http.StatusBadRequest)
		return
	}

	retentionDays, err := strconv.Atoi(r.FormValue("retention_days"))
	if err != nil || retentionDays < 1 || retentionDays > 3650 {
		http.Error(w, "retention_days must be between 1 and 3650", http.StatusBadRequest)
		return
	}

	if h.core != nil {
		// gRPC path — enrich context with admin identity so Core audit log records actor_user_id.
		grpcCtx := contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))
		_, err := h.core.UpdateServerConfig(grpcCtx, &pb.UpdateServerConfigRequest{
			InstanceName:            instanceName,
			RoomDefaultMaxMembers:   int32(maxRooms),
			AuditLogRetentionDays:   int32(retentionDays),
			// AllowRegistration: not in proto — UI-only field; no proto change needed for XS scope.
			// NOTE: AllowRegistration has no corresponding field in UpdateServerConfigRequest.
			// It remains a UI-only checkbox until the proto is extended in a follow-up story.
		})
		if err != nil {
			if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
				// Config row is always present (upsert semantics in Core) — NOT_FOUND is unexpected.
				slog.Error("admin: UpdateServerConfig returned NOT_FOUND (unexpected)", "err", err)
			} else {
				slog.Error("admin: UpdateServerConfig gRPC error", "err", err)
			}
			http.Redirect(w, r, "/admin/config?flash=Error+updating+config", http.StatusFound)
			return
		}
	} else {
		// stub fallback (nil client, unit-test path)
		stubConfig.InstanceName = instanceName
		stubConfig.AllowRegistration = r.FormValue("allow_registration") == "on"
		stubConfig.MaxRoomsPerUser = maxRooms
		stubConfig.RetentionDays = retentionDays
	}

	http.Redirect(w, r, "/admin/config?flash=Config+updated", http.StatusFound)
}
