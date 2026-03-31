package admin

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
)

// BootstrapStatusChecker checks whether the instance is in bootstrap mode.
type BootstrapStatusChecker interface {
	IsBootstrapActive(ctx context.Context) (bool, error)
}

// BootstrapHandler serves GET /admin/bootstrap and POST /admin/bootstrap (step navigation).
type BootstrapHandler struct {
	checker BootstrapStatusChecker
	tmpl    *TemplateHandler
}

// NewBootstrapHandler creates a BootstrapHandler with the given status checker and template handler.
func NewBootstrapHandler(checker BootstrapStatusChecker, tmpl *TemplateHandler) *BootstrapHandler {
	return &BootstrapHandler{checker: checker, tmpl: tmpl}
}

// Handler responds with the Bootstrap Wizard HTML page (step 1).
func (h *BootstrapHandler) Handler(w http.ResponseWriter, r *http.Request) {
	data := BootstrapPageData{
		PageData: PageData{
			BootstrapMode: true,
			ActiveNav:     "bootstrap",
		},
		Step: 1,
	}
	h.tmpl.render(w, "bootstrap", data)
}

// instanceNameRe validates instance name: 3–64 alphanumeric + hyphens.
var instanceNameRe = regexp.MustCompile(`^[a-zA-Z0-9-]{3,64}$`)

// StepHandler handles POST /admin/bootstrap — validates the current step and advances or re-renders.
func (h *BootstrapHandler) StepHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Collect accumulated state from form
	data := BootstrapPageData{
		PageData: PageData{
			BootstrapMode: true,
			ActiveNav:     "bootstrap",
		},
		InstanceName: r.FormValue("instance_name"),
		OIDCIssuer:   r.FormValue("oidc_issuer"),
		OIDCClientID: r.FormValue("oidc_client_id"),
		Errors:       make(map[string]string),
	}

	// Back navigation: if go_back is set, re-render the target step without validation.
	if goBack := r.FormValue("go_back"); goBack != "" {
		var targetStep int
		fmt.Sscan(goBack, &targetStep)
		if targetStep >= 1 && targetStep <= 4 {
			data.Step = targetStep
			h.tmpl.render(w, "bootstrap", data)
			return
		}
	}

	// Parse current step
	var currentStep int
	fmt.Sscan(r.FormValue("step"), &currentStep)
	if currentStep < 1 || currentStep > 4 {
		currentStep = 1
	}

	switch currentStep {
	case 1:
		// Validate instance_name
		if !instanceNameRe.MatchString(data.InstanceName) {
			data.Errors["instance_name"] = "Instance name must be 3–64 characters, alphanumeric and hyphens only."
			data.Step = 1
			w.WriteHeader(http.StatusUnprocessableEntity)
			h.tmpl.render(w, "bootstrap", data)
			return
		}
		data.Step = 2

	case 2:
		// Validate OIDC fields
		if data.OIDCIssuer == "" {
			data.Errors["oidc_issuer"] = "OIDC Issuer URL is required."
		} else {
			parsed, err := url.ParseRequestURI(data.OIDCIssuer)
			if err != nil || parsed.Scheme != "https" {
				data.Errors["oidc_issuer"] = "OIDC Issuer must be a valid HTTPS URL."
			}
		}
		if r.FormValue("oidc_client_id") == "" {
			data.Errors["oidc_client_id"] = "OIDC Client ID is required."
		}
		if r.FormValue("oidc_client_secret") == "" {
			data.Errors["oidc_client_secret"] = "OIDC Client Secret is required."
		}
		if len(data.Errors) > 0 {
			data.Step = 2
			w.WriteHeader(http.StatusUnprocessableEntity)
			h.tmpl.render(w, "bootstrap", data)
			return
		}
		data.Step = 3

	case 3:
		// No form validation for step 3 (keys generated via async fetch)
		data.Step = 4

	case 4:
		// Final submit — Story 3.8 replaces with real persistence; stub redirect for now
		slog.Info("bootstrap wizard step 4 submitted — stub: redirecting to /admin/login")
		http.Redirect(w, r, "/admin/login", http.StatusFound)
		return
	}

	h.tmpl.render(w, "bootstrap", data)
}

// PostgresBootstrapChecker checks bootstrap status against PostgreSQL.
type PostgresBootstrapChecker struct {
	db *sql.DB
}

// NewPostgresBootstrapChecker creates a checker backed by the given DB connection.
func NewPostgresBootstrapChecker(db *sql.DB) *PostgresBootstrapChecker {
	return &PostgresBootstrapChecker{db: db}
}

// IsBootstrapActive returns true when the instance is in bootstrap mode:
//   - bootstrap_completed exists → false
//   - bootstrap_active exists (no bootstrap_completed) → true
//   - neither exists and no users → true (pre-first-login)
//   - neither exists and users exist → false
func (c *PostgresBootstrapChecker) IsBootstrapActive(ctx context.Context) (bool, error) {
	rows, err := c.db.QueryContext(ctx,
		"SELECT key, value FROM server_config WHERE key IN ('bootstrap_active', 'bootstrap_completed')")
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var hasActive, hasCompleted bool
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return false, err
		}
		switch key {
		case "bootstrap_active":
			hasActive = true
		case "bootstrap_completed":
			hasCompleted = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	if hasCompleted {
		return false, nil
	}
	if hasActive {
		return true, nil
	}

	// Neither flag exists — check if any users exist
	var usersExist bool
	err = c.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users)").Scan(&usersExist)
	if err != nil {
		return false, err
	}

	return !usersExist, nil
}
