package admin

import (
	"net/http"
	"strconv"
	"strings"
)

// ConfigHandler serves the Server Configuration page (Story 7.10).
type ConfigHandler struct {
	tmpl *TemplateHandler
}

// NewConfigHandler creates a ConfigHandler with the given template handler.
func NewConfigHandler(tmpl *TemplateHandler) *ConfigHandler {
	return &ConfigHandler{tmpl: tmpl}
}

// Handler serves GET /admin/config.
// Renders config.html with ConfigPageData populated from stubConfig.
// Reads the ?flash= query param and populates Flash with an AlertBanner on success.
func (h *ConfigHandler) Handler(w http.ResponseWriter, r *http.Request) {
	var flash AlertBannerData
	if msg := sanitizeFlash(r.URL.Query().Get("flash")); msg != "" {
		flash = AlertBannerData{Severity: "success", Message: msg, Dismissible: true}
	}
	data := ConfigPageData{
		PageData: PageData{ActiveNav: "config", CSRFToken: CSRFTokenFromContext(r.Context())},
		Config:   stubConfig,
		Flash:    flash,
	}
	h.tmpl.render(w, "config", data)
}

// UpdateConfigHandler handles POST /admin/config.
// Validates form fields, updates stubConfig in-memory, then PRG-redirects.
// TODO(epic-6): replace stub mutation with Admin API call (PATCH /api/v1/admin/config).
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

	// TODO(epic-6): replace stub mutation with Admin API call (PATCH /api/v1/admin/config)
	stubConfig.InstanceName = instanceName
	stubConfig.AllowRegistration = r.FormValue("allow_registration") == "on"
	stubConfig.MaxRoomsPerUser = maxRooms
	stubConfig.RetentionDays = retentionDays

	http.Redirect(w, r, "/admin/config?flash=Config+updated", http.StatusFound)
}
