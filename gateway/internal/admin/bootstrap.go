package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"
)

// maskSecret returns a display-safe masked representation of the secret.
// For secrets of 6+ chars: first 3 + "..." + last 3. Shorter: "***".
func maskSecret(s string) string {
	if len(s) < 6 {
		return "***"
	}
	return s[:3] + "..." + s[len(s)-3:]
}

// BootstrapStatusChecker checks whether the instance is in bootstrap mode.
type BootstrapStatusChecker interface {
	IsBootstrapActive(ctx context.Context) (bool, error)
}

// BootstrapPersister persists the bootstrap configuration to the database.
type BootstrapPersister interface {
	SaveBootstrapConfig(ctx context.Context, instanceName, oidcIssuer, oidcClientID, encryptedSecret string) error
}

// BootstrapDraftStore reads and writes wizard draft data to/from the database.
type BootstrapDraftStore interface {
	SaveDraft(ctx context.Context, key, value string) error
	LoadDraft(ctx context.Context, key string) (string, bool, error) // returns value, found, error
	ClearDraft(ctx context.Context) error
}

// postgresBootstrapDraftStore implements BootstrapDraftStore using PostgreSQL.
type postgresBootstrapDraftStore struct {
	db *sql.DB
}

func (s *postgresBootstrapDraftStore) SaveDraft(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO bootstrap_draft (key, value, set_at) VALUES ($1, $2, $3)
         ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at`,
		key, value, time.Now().UnixMilli(),
	)
	return err
}

func (s *postgresBootstrapDraftStore) LoadDraft(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM bootstrap_draft WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *postgresBootstrapDraftStore) ClearDraft(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM bootstrap_draft`)
	return err
}

// BootstrapHandler serves GET /admin/bootstrap and POST /admin/bootstrap (step navigation).
type BootstrapHandler struct {
	checker    BootstrapStatusChecker
	tmpl       *TemplateHandler
	persister  BootstrapPersister
	draftStore BootstrapDraftStore
	secret     []byte
}

// NewBootstrapHandler creates a BootstrapHandler with the given status checker, template handler, db, and secret.
func NewBootstrapHandler(checker BootstrapStatusChecker, tmpl *TemplateHandler, db *sql.DB, secret []byte) *BootstrapHandler {
	return &BootstrapHandler{
		checker:    checker,
		tmpl:       tmpl,
		persister:  &postgresBootstrapPersister{db: db},
		draftStore: &postgresBootstrapDraftStore{db: db},
		secret:     secret,
	}
}

// Handler responds with the Bootstrap Wizard HTML page (step 1).
func (h *BootstrapHandler) Handler(w http.ResponseWriter, r *http.Request) {
	data := BootstrapPageData{
		PageData: PageData{
			BootstrapMode: true,
			ActiveNav:     "bootstrap",
			CSRFToken:     CSRFTokenFromContext(r.Context()),
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
			CSRFToken:     CSRFTokenFromContext(r.Context()),
		},
		InstanceName: r.FormValue("instance_name"),
		OIDCIssuer:   r.FormValue("oidc_issuer"),
		OIDCClientID: r.FormValue("oidc_client_id"),
		Errors:       make(map[string]string),
		Warnings:     make(map[string]string),
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
		if err := h.draftStore.SaveDraft(r.Context(), "instance_name", data.InstanceName); err != nil {
			slog.Error("failed to save draft", "step", 1, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		data.Step = 2

	case 2:
		// Validate OIDC fields
		if data.OIDCIssuer == "" {
			data.Errors["oidc_issuer"] = "OIDC Issuer URL is required."
		} else if err := validateIssuerURL(data.OIDCIssuer); err != nil {
			http.Error(w, `{"errcode":"M_BAD_JSON","error":"OIDC issuer must use HTTPS (http://localhost allowed for dev)"}`, http.StatusBadRequest)
			return
		}
		if r.FormValue("oidc_client_id") == "" {
			data.Errors["oidc_client_id"] = "OIDC Client ID is required."
		}
		clientSecret := r.FormValue("oidc_client_secret")
		if clientSecret == "" {
			data.Errors["oidc_client_secret"] = "OIDC Client Secret is required."
		}
		if len(data.Errors) > 0 {
			data.Step = 2
			w.WriteHeader(http.StatusUnprocessableEntity)
			h.tmpl.render(w, "bootstrap", data)
			return
		}
		// Encrypt the OIDC client secret before storing in DB draft
		encryptedSecret, err := encryptAES256GCM(h.secret, clientSecret)
		if err != nil {
			slog.Error("failed to encrypt draft secret", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		draftFields := []struct{ key, value string }{
			{"oidc_issuer", data.OIDCIssuer},
			{"oidc_client_id", r.FormValue("oidc_client_id")},
			{"oidc_client_secret", encryptedSecret},
		}
		for _, f := range draftFields {
			if err := h.draftStore.SaveDraft(r.Context(), f.key, f.value); err != nil {
				slog.Error("failed to save draft", "step", 2, "key", f.key, "err", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}
		// Redirect to OIDC login — the callback will show claim selection.
		http.Redirect(w, r, "/admin/login/start?mode=bootstrap", http.StatusSeeOther)
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

// postgresBootstrapPersister persists bootstrap configuration to PostgreSQL.
type postgresBootstrapPersister struct {
	db *sql.DB
}

// SaveBootstrapConfig inserts all bootstrap config rows in a single transaction.
func (p *postgresBootstrapPersister) SaveBootstrapConfig(ctx context.Context, instanceName, oidcIssuer, oidcClientID, encryptedSecret string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if err := saveBootstrapConfigTx(ctx, tx, instanceName, oidcIssuer, oidcClientID, encryptedSecret); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// saveBootstrapConfigTx writes the bootstrap config rows via the given sqlQuerier
// (which may be a *sql.Tx or a *sql.DB). No commit/rollback is performed here.
func saveBootstrapConfigTx(ctx context.Context, q sqlQuerier, instanceName, oidcIssuer, oidcClientID, encryptedSecret string) error {
	nowMs := time.Now().UnixMilli()
	// Note: bootstrap_completed is NOT set here — it is written by ClaimSelectionHandler
	// after the first successful admin login (mode=bootstrap), ensuring the admin
	// identity is confirmed before the instance is considered fully bootstrapped.
	rows := []struct{ key, value string }{
		{"instance_name", instanceName},
		{"oidc_issuer", oidcIssuer},
		{"oidc_client_id", oidcClientID},
		{"oidc_client_secret", encryptedSecret},
	}

	for _, row := range rows {
		if _, err := q.ExecContext(ctx,
			`INSERT INTO server_config (key, value, set_at) VALUES ($1, $2, $3)
             ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at`,
			row.key, row.value, nowMs,
		); err != nil {
			return fmt.Errorf("inserting %s: %w", row.key, err)
		}
	}

	return nil
}

// clearDraftTx deletes all bootstrap_draft rows via the given sqlQuerier.
func clearDraftTx(ctx context.Context, q sqlQuerier) error {
	_, err := q.ExecContext(ctx, `DELETE FROM bootstrap_draft`)
	return err
}
