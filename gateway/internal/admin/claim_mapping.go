package admin

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/nebu/nebu/internal/audit"
	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// oidcClaimNameRe validates OIDC claim names for the claim-mapping settings page.
// Allows letters, digits, colon, underscore, hyphen, and dot (for nested claims like user.email).
// DO NOT modify oidcGroupClaimRe in role_mapping.go — that regex intentionally excludes dots.
//
// Security: claim names are used as map keys in JWT claim extraction, not in SQL.
// Validation prevents garbage/injection values from reaching server_config.
// Actual regex string: ^[a-zA-Z0-9:_\-.]+$
var oidcClaimNameRe = regexp.MustCompile(`^[a-zA-Z0-9:_\-.]+$`)

// ClaimMappingHandler serves the OIDC Claim Mapping configuration page (Story 11-10).
// GET  /admin/config/claim-mapping — shows current values or Nebu defaults.
// POST /admin/config/claim-mapping — validates, saves, PRG redirect with flash.
type ClaimMappingHandler struct {
	tmpl         *TemplateHandler
	configReader ServerConfigReader
	coreClient   pb.CoreServiceClient // optional; nil disables audit logging
}

// SetCoreClient injects the gRPC core client for audit log calls.
// Call after NewClaimMappingHandler before the HTTP server starts.
func (h *ClaimMappingHandler) SetCoreClient(c pb.CoreServiceClient) {
	h.coreClient = c
}

// NewClaimMappingHandler creates a ClaimMappingHandler.
// configReader is required; pass a real postgresServerConfigReader in production and
// a test double (fakeServerConfigReader) in unit tests.
func NewClaimMappingHandler(tmpl *TemplateHandler, configReader ServerConfigReader) *ClaimMappingHandler {
	return &ClaimMappingHandler{tmpl: tmpl, configReader: configReader}
}

// Handler serves GET /admin/config/claim-mapping.
// Reads current values from server_config; falls back to defaults (name, name, email) if absent.
func (h *ClaimMappingHandler) Handler(w http.ResponseWriter, r *http.Request) {
	var flash AlertBannerData
	if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != "" {
		flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
	}

	userIDClaim, displaynameClaim, emailClaim := "name", "name", "email"
	if uid, dn, em, err := h.configReader.LoadClaimMapping(r.Context()); err != nil {
		slog.Warn("failed to load claim mapping, showing defaults", "err", err)
		// Override any existing flash to surface the read failure so the admin
		// knows the displayed values may not reflect what is stored in the DB.
		flash = AlertBannerData{
			Severity:    "warning",
			Message:     "Could not load current values — showing defaults. Check server logs.",
			Dismissible: true,
		}
	} else {
		userIDClaim, displaynameClaim, emailClaim = uid, dn, em
	}

	// Detect bootstrap state: if bootstrap_completed key is set, lock the user-ID claim field.
	bootstrapCompleted := false
	if v, err := h.configReader.LoadServerConfigKey(r.Context(), "bootstrap_completed"); err == nil && v != "" {
		bootstrapCompleted = true
	}

	pd := newPageData()
	pd.ActiveNav = "claim-mapping"
	pd.CSRFToken = CSRFTokenFromContext(r.Context())
	data := ClaimMappingPageData{
		PageData:           pd,
		UserIDClaim:        userIDClaim,
		DisplaynameClaim:   displaynameClaim,
		EmailClaim:         emailClaim,
		Flash:              flash,
		BootstrapCompleted: bootstrapCompleted,
	}
	h.tmpl.render(w, "claim-mapping", data)
}

// UpdateHandler handles POST /admin/config/claim-mapping.
// Validates form fields and writes the values to server_config via SaveClaimMapping.
// On success: PRG redirect to the page with ?flash=Claim+mapping+updated.
// On failure: HTTP 422 with form re-rendered showing per-field errors.
//
// NOTE(security): Changing oidc_user_id_claim after bootstrap is irreversible for existing
// users — it generates different Matrix user IDs for the same OIDC subjects, breaking all
// their existing room memberships. The template displays a prominent warning about this risk.
func (h *ClaimMappingHandler) UpdateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	displaynameClaim := strings.TrimSpace(r.FormValue("oidc_displayname_claim"))
	emailClaim := strings.TrimSpace(r.FormValue("oidc_email_claim"))

	// Defense in depth: if bootstrap is complete, preserve the existing oidc_user_id_claim
	// regardless of what the form posted. The template already hides the input, so this is
	// a safeguard against direct POST requests bypassing the UI.
	bootstrapCompleted := false
	if v, err := h.configReader.LoadServerConfigKey(r.Context(), "bootstrap_completed"); err == nil && v != "" {
		bootstrapCompleted = true
	}

	var userIDClaim string
	if bootstrapCompleted {
		// Load the locked value from DB — if this fails, abort rather than saving an empty claim.
		// Saving an empty oidc_user_id_claim would break all future logins; abort is safer.
		uid, _, _, err := h.configReader.LoadClaimMapping(r.Context())
		if err != nil {
			slog.Error("claim_mapping UpdateHandler: failed to load locked oidc_user_id_claim post-bootstrap", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		userIDClaim = uid
	} else {
		userIDClaim = strings.TrimSpace(r.FormValue("oidc_user_id_claim"))
	}

	errors := make(map[string]string)
	if !bootstrapCompleted {
		validateClaimField(userIDClaim, "oidc_user_id_claim", errors)
	}
	validateClaimField(displaynameClaim, "oidc_displayname_claim", errors)
	validateClaimField(emailClaim, "oidc_email_claim", errors)

	if len(errors) > 0 {
		pd := newPageData()
		pd.ActiveNav = "claim-mapping"
		pd.CSRFToken = CSRFTokenFromContext(r.Context())
		data := ClaimMappingPageData{
			PageData:           pd,
			UserIDClaim:        userIDClaim,
			DisplaynameClaim:   displaynameClaim,
			EmailClaim:         emailClaim,
			Errors:             errors,
			BootstrapCompleted: bootstrapCompleted,
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		h.tmpl.render(w, "claim-mapping", data)
		return
	}

	// Read prior values before saving so the audit log can record before/after.
	// Fail-open: if LoadClaimMapping errors we proceed with empty prior values rather than
	// aborting a valid save — the admin's intent is clear even without before state.
	prevUID, prevDN, prevEM := "", "", ""
	if uid, dn, em, err := h.configReader.LoadClaimMapping(r.Context()); err == nil {
		prevUID, prevDN, prevEM = uid, dn, em
	}

	if err := h.configReader.SaveClaimMapping(r.Context(), userIDClaim, displaynameClaim, emailClaim); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Audit log: claim mapping change is security-relevant (identity stability risk — AC note).
	// Fail-open: audit.LogEvent's never-raise contract means gRPC errors are logged and swallowed.
	if h.coreClient != nil {
		actorSub := AdminSubFromContext(r.Context())
		audit.LogEvent(r.Context(), h.coreClient, actorSub, "claim_mapping_updated", "server_config", "",
			map[string]any{
				"oidc_user_id_claim":              userIDClaim,
				"oidc_displayname_claim":          displaynameClaim,
				"oidc_email_claim":                emailClaim,
				"previous_oidc_user_id_claim":     prevUID,
				"previous_oidc_displayname_claim": prevDN,
				"previous_oidc_email_claim":       prevEM,
			}, "success", "") //nolint:errcheck // never-raise contract
	}

	http.Redirect(w, r, "/admin/config/claim-mapping?flash=Claim+mapping+updated", http.StatusFound)
}

// validateClaimField checks that a claim name is non-empty, at most 50 chars, and matches
// the oidcClaimNameRe allowlist. Errors are written to the provided map.
func validateClaimField(value, field string, errors map[string]string) {
	if value == "" {
		errors[field] = "Claim name must not be empty."
		return
	}
	if utf8.RuneCountInString(value) > 50 {
		errors[field] = "Claim name must not exceed 50 characters."
		return
	}
	if !oidcClaimNameRe.MatchString(value) {
		errors[field] = "Claim name may only contain letters, digits, colons, underscores, hyphens, and dots."
	}
}
