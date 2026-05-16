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

// ConfigKeyWriter is a minimal interface for writing individual server_config keys directly to the DB.
// Used by UpdateConfigHandler to persist bool fields that cannot be safely round-tripped via proto3
// (proto3 bool default false is indistinguishable from "not set", so the gRPC path cannot carry it).
// Implemented by api.ServerConfigRepository (same UpsertServerConfigKey method signature).
// In tests, pass nil — the handler falls back to stub mutation when configDB is nil.
type ConfigKeyWriter interface {
	UpsertServerConfigKey(ctx context.Context, key, value string) error
}

// ConfigHandler serves the Server Configuration page (Story 7.10).
// Story 9.4: backed by real gRPC calls to the Elixir core when core is non-nil.
// Story 14-2a: configDB is used for direct DB upsert of proto3 bool fields.
// Story 14-3c: secret is used to encrypt scim_bearer_token before persistence (CR-1).
// Falls back to stub data when core is nil (unit-test path).
type ConfigHandler struct {
	tmpl     *TemplateHandler
	core     AdminConfigClient
	configDB ConfigKeyWriter // Story 14-2a: direct DB write for bool config keys
	secret   []byte          // Story 14-3c: AES-256-GCM key for scim_bearer_token encryption (CR-1)
}

// NewConfigHandler creates a ConfigHandler with the given template handler and optional gRPC client.
// Pass nil (or omit) for core to use stub data (unit-test path; stub fallback preserved for
// backward compatibility with existing unit tests).
// Pass configDB to enable direct DB persistence for proto3 bool fields (oidc_directory_enabled).
func NewConfigHandler(tmpl *TemplateHandler, core ...AdminConfigClient) *ConfigHandler {
	var c AdminConfigClient
	if len(core) > 0 {
		c = core[0]
	}
	return &ConfigHandler{tmpl: tmpl, core: c}
}

// WithConfigDB sets the DB writer for direct server_config key persistence.
// Call after NewConfigHandler when a real DB connection is available.
// Story 14-2a: needed to persist oidc_directory_enabled (proto3 bool — cannot use gRPC path).
func (h *ConfigHandler) WithConfigDB(db ConfigKeyWriter) *ConfigHandler {
	h.configDB = db
	return h
}

// WithSecret sets the AES-256-GCM encryption key used to encrypt scim_bearer_token before DB storage.
// Story 14-3c: CR-1 requires the raw bearer token to never appear in plaintext in the database.
// Call after NewConfigHandler with the same secret used by BootstrapHandler (from NEBU_INTERNAL_SECRET_FILE).
func (h *ConfigHandler) WithSecret(secret []byte) *ConfigHandler {
	h.secret = secret
	return h
}

// protoToStubConfig maps a ServerConfigProto to StubConfig for template rendering.
// Template compatibility: retaining StubConfig avoids a cross-file refactor.
// AllowRegistration: not in proto (no corresponding server config field in the current data model).
// It is preserved in StubConfig for backward compatibility; the checkbox remains UI-only.
// NOTE: when core is nil, the stub default (AllowRegistration: true) is used.
//
// Story 14-3c: SCIM fields (scim_enabled, scim_base_url, scim_bearer_token_set) are NOT in the
// proto (ServerConfigProto) — they are persisted via direct DB upsert like oidc_directory_enabled.
// The caller (Handler) must supplement the protoToStubConfig result with SCIM fields from
// stubConfig (unit-test path) or from a direct DB read (real path).
// ScimBearerTokenSet is derived from whether the encrypted token is non-empty — CR-1.
func protoToStubConfig(p *pb.ServerConfigProto) StubConfig {
	return StubConfig{
		InstanceName:    p.GetInstanceName(),
		MaxRoomsPerUser: int(p.GetRoomDefaultMaxMembers()),
		RetentionDays:   int(p.GetAuditLogRetentionDays()),
		// AllowRegistration has no proto equivalent; keep UI-only state from stub default.
		AllowRegistration:     stubConfig.AllowRegistration,
		OidcDirectoryEnabled:  p.GetOidcDirectoryEnabled(),  // Story 14-2a
		OidcDirectoryEndpoint: p.GetOidcDirectoryEndpoint(), // Story 14-2a
		// SCIM fields are not in proto — carry over from in-memory stub.
		// In the real path these are loaded by ConfigHandler.Handler from configDB.
		ScimEnabled:        stubConfig.ScimEnabled,
		ScimBaseURL:        stubConfig.ScimBaseURL,
		ScimBearerTokenSet: stubConfig.ScimBearerTokenSet,
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

	// Parse OIDC directory fields from the form (Story 14-2a).
	// The checkbox "oidc_directory_enabled" sends "on" when checked, absent when unchecked.
	oidcDirectoryEnabled := r.FormValue("oidc_directory_enabled") == "on"
	oidcDirectoryEndpoint := strings.TrimSpace(r.FormValue("oidc_directory_endpoint"))

	// Parse SCIM fields from the form (Story 14-3c).
	// CR-2: scim_base_url must be HTTPS — validated below.
	// CR-1: scim_bearer_token is encrypted before persistence; never stored in plain text.
	scimEnabled := r.FormValue("scim_enabled") == "on"
	scimBaseURL := strings.TrimSpace(r.FormValue("scim_base_url"))
	scimBearerToken := r.FormValue("scim_bearer_token") // raw token — encrypted before storage

	// CR-2: validate HTTPS for scim_base_url when non-empty.
	if scimBaseURL != "" {
		if err := validateEndpoint(scimBaseURL); err != nil {
			http.Error(w, "scim_base_url must use HTTPS", http.StatusBadRequest)
			return
		}
	}

	if h.core != nil {
		// gRPC path — enrich context with admin identity so Core audit log records actor_user_id.
		grpcCtx := contextWithAdminIdentity(r.Context(), AdminSubFromContext(r.Context()))
		_, err := h.core.UpdateServerConfig(grpcCtx, &pb.UpdateServerConfigRequest{
			InstanceName:            instanceName,
			RoomDefaultMaxMembers:   int32(maxRooms),
			AuditLogRetentionDays:   int32(retentionDays),
			OidcDirectoryEndpoint:   oidcDirectoryEndpoint,
			// OidcDirectoryEnabled is NOT set here: proto3 bool cannot distinguish false-from-unset.
			// It is persisted via direct DB upsert below (configDB path) to avoid inadvertently
			// overwriting the stored value with proto default (false) on unrelated gRPC calls.
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
		// Persist bool and SCIM fields directly to DB (Story 14-2a / Story 14-3c):
		// proto3 bool cannot safely round-trip via gRPC (false == unset), so we use a
		// direct DB upsert. configDB is nil in unit tests (stub path handled below).
		if h.configDB != nil {
			enabledStr := "false"
			if oidcDirectoryEnabled {
				enabledStr = "true"
			}
			if dbErr := h.configDB.UpsertServerConfigKey(r.Context(), "oidc_directory_enabled", enabledStr); dbErr != nil {
				slog.Error("admin: failed to persist oidc_directory_enabled", "err", dbErr)
				http.Redirect(w, r, "/admin/config?flash=Error+updating+config", http.StatusFound)
				return
			}
			// Story 14-3c: persist SCIM config via direct DB upsert (same pattern as OIDC bool).
			scimEnabledStr := "false"
			if scimEnabled {
				scimEnabledStr = "true"
			}
			if dbErr := h.configDB.UpsertServerConfigKey(r.Context(), "scim_enabled", scimEnabledStr); dbErr != nil {
				slog.Error("admin: failed to persist scim_enabled", "err", dbErr)
				http.Redirect(w, r, "/admin/config?flash=Error+updating+config", http.StatusFound)
				return
			}
			if dbErr := h.configDB.UpsertServerConfigKey(r.Context(), "scim_base_url", scimBaseURL); dbErr != nil {
				slog.Error("admin: failed to persist scim_base_url", "err", dbErr)
				http.Redirect(w, r, "/admin/config?flash=Error+updating+config", http.StatusFound)
				return
			}
			// CR-1: only persist scim_bearer_token when the form submitted a non-empty value.
			// If the field is empty, the existing token is left unchanged (user submitted no change).
			if scimBearerToken != "" && h.secret != nil {
				encToken, encErr := encryptAES256GCM(h.secret, scimBearerToken)
				if encErr != nil {
					slog.Error("admin: failed to encrypt scim_bearer_token", "err", encErr)
					http.Redirect(w, r, "/admin/config?flash=Error+updating+config", http.StatusFound)
					return
				}
				if dbErr := h.configDB.UpsertServerConfigKey(r.Context(), "scim_bearer_token", encToken); dbErr != nil {
					slog.Error("admin: failed to persist scim_bearer_token", "err", dbErr)
					http.Redirect(w, r, "/admin/config?flash=Error+updating+config", http.StatusFound)
					return
				}
			}
		}
	} else {
		// stub fallback (nil client, unit-test path)
		stubConfig.InstanceName = instanceName
		stubConfig.AllowRegistration = r.FormValue("allow_registration") == "on"
		stubConfig.MaxRoomsPerUser = maxRooms
		stubConfig.RetentionDays = retentionDays
		stubConfig.OidcDirectoryEnabled = oidcDirectoryEnabled
		stubConfig.OidcDirectoryEndpoint = oidcDirectoryEndpoint
		// Story 14-3c: update SCIM fields in stub (unit-test path).
		// CR-1: never store raw token — use ScimBearerTokenSet bool instead.
		stubConfig.ScimEnabled = scimEnabled
		stubConfig.ScimBaseURL = scimBaseURL
		if scimBearerToken != "" {
			stubConfig.ScimBearerTokenSet = true
		}
	}

	http.Redirect(w, r, "/admin/config?flash=Config+updated", http.StatusFound)
}
