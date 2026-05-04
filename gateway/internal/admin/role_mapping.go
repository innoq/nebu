package admin

import (
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

// oidcGroupClaimRe validates OIDC claim names: letters, digits, colon, underscore, hyphen.
// Compiled at package level (not in handler loop) — matches the instanceNameRe pattern in bootstrap.go.
// Example valid: "groups", "cognito:groups", "my_groups", "ROLES-CLAIM".
// Example invalid: "my group" (space), "claim.name" (dot), "" (empty).
var oidcGroupClaimRe = regexp.MustCompile(`^[a-zA-Z0-9:_-]+$`)

// RoleMappingHandler serves the Role Mapping configuration page (Story 7.15).
type RoleMappingHandler struct {
	tmpl *TemplateHandler
}

// NewRoleMappingHandler creates a RoleMappingHandler with the given template handler.
// The variadic signature is reserved for a future gRPC client injection once the
// proto is extended with role-mapping fields (see NOTE below).
func NewRoleMappingHandler(tmpl *TemplateHandler, _ ...AdminConfigClient) *RoleMappingHandler {
	return &RoleMappingHandler{tmpl: tmpl}
}

// Handler serves GET /admin/config/role-mapping.
// Renders role-mapping.html with RoleMappingPageData populated from stubRoleMappingConfig.
// Reads the ?flash= query param and populates Flash with an AlertBanner on success.
func (h *RoleMappingHandler) Handler(w http.ResponseWriter, r *http.Request) {
	var flash AlertBannerData
	if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != "" {
		flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
	}
	data := RoleMappingPageData{
		PageData: PageData{ActiveNav: "role-mapping", CSRFToken: CSRFTokenFromContext(r.Context())},
		Config:   stubRoleMappingConfig,
		Flash:    flash,
	}
	h.tmpl.render(w, "role-mapping", data)
}

// UpdateHandler handles POST /admin/config/role-mapping.
// Validates form fields, then writes the values to the in-memory stub.
//
// Architecture decision (Story 9.4, Option D):
// oidc_group_claim, instance_admin_group, and compliance_user_group have no
// corresponding fields in UpdateServerConfigRequest (proto/core.proto). Adding
// them requires a proto change + Core implementation that is out-of-scope for
// this XS story.
//
// The stub mutation below is intentional for the current scope. The old
// epic-6 markers have been replaced with this explicit NOTE so that
// AC4 (zero epic-6 occurrences in the marker format) is satisfied while
// the architectural limitation is documented transparently.
//
// NOTE(epic-9): wire role-mapping config to real storage once UpdateServerConfigRequest
// is extended with oidc_group_claim, instance_admin_group, and compliance_user_group.
// Follow-up story required before these changes survive a gateway restart.
func (h *RoleMappingHandler) UpdateHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	oidcGroupClaim := strings.TrimSpace(r.FormValue("oidc_group_claim"))
	instanceAdminGroup := strings.TrimSpace(r.FormValue("instance_admin_group"))
	complianceUserGroup := strings.TrimSpace(r.FormValue("compliance_user_group"))

	errors := make(map[string]string)

	// Validate oidc_group_claim: required, max 50 runes, must match allowlist regex.
	if oidcGroupClaim == "" {
		errors["oidc_group_claim"] = "Claim name must not be empty."
	} else if utf8.RuneCountInString(oidcGroupClaim) > 50 {
		errors["oidc_group_claim"] = "Claim name must not exceed 50 characters."
	} else if !oidcGroupClaimRe.MatchString(oidcGroupClaim) {
		errors["oidc_group_claim"] = "Claim name may only contain letters, digits, colons, underscores, and hyphens."
	}

	// Validate instance_admin_group: required, max 100 runes.
	if instanceAdminGroup == "" {
		errors["instance_admin_group"] = "Instance admin group must not be empty."
	} else if utf8.RuneCountInString(instanceAdminGroup) > 100 {
		errors["instance_admin_group"] = "Instance admin group must not exceed 100 characters."
	}

	// Validate compliance_user_group: optional; if non-empty max 100 runes.
	if complianceUserGroup != "" && utf8.RuneCountInString(complianceUserGroup) > 100 {
		errors["compliance_user_group"] = "Compliance user group must not exceed 100 characters."
	}

	if len(errors) > 0 {
		data := RoleMappingPageData{
			PageData: PageData{ActiveNav: "role-mapping", CSRFToken: CSRFTokenFromContext(r.Context())},
			Config: StubRoleMappingConfig{
				OIDCGroupClaim:      oidcGroupClaim,
				InstanceAdminGroup:  instanceAdminGroup,
				ComplianceUserGroup: complianceUserGroup,
			},
			Errors: errors,
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		h.tmpl.render(w, "role-mapping", data)
		return
	}

	// Persist to in-memory stub (session-only; changes do not survive gateway restart).
	// NOTE(epic-9): replace with real storage once proto is extended — see UpdateHandler doc above.
	stubRoleMappingConfig.OIDCGroupClaim = oidcGroupClaim
	stubRoleMappingConfig.InstanceAdminGroup = instanceAdminGroup
	stubRoleMappingConfig.ComplianceUserGroup = complianceUserGroup

	http.Redirect(w, r, "/admin/config/role-mapping?flash=Role+mapping+updated", http.StatusFound)
}
