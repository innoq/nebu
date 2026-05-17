package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// OIDCDirectoryFetcher is the interface used by BootstrapHandler to access the OIDC directory.
// It is satisfied by *OIDCDirectoryService (from oidc_directory.go).
// Defined as a narrow interface here so tests can inject fakes without a real HTTP server.
type OIDCDirectoryFetcher interface {
	IsEnabled() bool
	FetchUsers(ctx context.Context) ([]OIDCDirectoryUser, error)
}

// BulkImportClient is the interface used by BootstrapHandler to call the BulkImportUsers gRPC RPC.
// It is satisfied by the generated CoreServiceClient from gateway/internal/grpc/pb.
type BulkImportClient interface {
	BulkImportUsers(ctx context.Context, req *pb.BulkImportUsersRequest, opts ...grpc.CallOption) (*pb.BulkImportUsersResponse, error)
}

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

	// Step 4 (Story 14-3b): User Import services. Optional — nil = Step 4 renders with disabled button.
	// oidcFetcher provides IsEnabled() and FetchUsers() for the preview step.
	// core is the gRPC client for BulkImportUsers.
	// serverName is the Matrix server name (e.g. "example.com") for Matrix User ID computation.
	oidcFetcher OIDCDirectoryFetcher
	core        BulkImportClient
	serverName  string

	// Step 4 (Story 14-3c): SCIM 2.0 fetcher. When set and IsEnabled() == true, SCIM
	// takes priority over oidcFetcher for the action=import flow (AC1).
	// Defined via WithSCIMFetcher (bootstrap_scim.go).
	scimFetcher SCIMFetcher
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

// WithImportServices wires Step 4 (User Import) services into the handler.
// Call after NewBootstrapHandler. Returns the handler for fluent chaining.
//
// oidcFetcher — provides IsEnabled() and FetchUsers() for the Step 4 preview;
//               pass nil to render Step 4 with the disabled-button state (AC4).
// core       — gRPC client for BulkImportUsers; pass nil to disable the import action.
// serverName — Matrix server name (e.g. "example.com") used to compute @localpart:server Matrix IDs.
func (h *BootstrapHandler) WithImportServices(oidcFetcher OIDCDirectoryFetcher, core BulkImportClient, serverName string) *BootstrapHandler {
	h.oidcFetcher = oidcFetcher
	h.core = core
	h.serverName = serverName
	return h
}

// Handler responds with the Bootstrap Wizard HTML page.
// Normally renders Step 1. When ?step=4 is present in the query string,
// renders Step 4 (User Import) — used after the OIDC callback redirect.
func (h *BootstrapHandler) Handler(w http.ResponseWriter, r *http.Request) {
	bootstrapPD := newPageData()
	bootstrapPD.BootstrapMode = true
	bootstrapPD.ActiveNav = "bootstrap"
	bootstrapPD.CSRFToken = CSRFTokenFromContext(r.Context())

	// AC1 (Story 14-3b): support GET /admin/bootstrap?step=4 for the post-OIDC redirect.
	// Story 14-3c: OIDCDirectoryEnabled is true when either the OIDC directory fetcher
	// OR the SCIM fetcher is enabled — both protocols provide user-import capability.
	if r.URL.Query().Get("step") == "4" {
		oidcEnabled := h.oidcFetcher != nil && h.oidcFetcher.IsEnabled()
		scimEnabled := h.scimFetcher != nil && h.scimFetcher.IsEnabled()
		data := BootstrapPageData{
			PageData:             bootstrapPD,
			Step:                 4,
			OIDCDirectoryEnabled: oidcEnabled || scimEnabled,
		}
		h.tmpl.render(w, "bootstrap", data)
		return
	}

	data := BootstrapPageData{
		PageData: bootstrapPD,
		Step:     1,
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
	bootstrapStepPD := newPageData()
	bootstrapStepPD.BootstrapMode = true
	bootstrapStepPD.ActiveNav = "bootstrap"
	bootstrapStepPD.CSRFToken = CSRFTokenFromContext(r.Context())
	data := BootstrapPageData{
		PageData:     bootstrapStepPD,
		InstanceName: r.FormValue("instance_name"),
		OIDCIssuer:   r.FormValue("oidc_issuer"),
		OIDCClientID: r.FormValue("oidc_client_id"),
		Errors:       make(map[string]string),
		Warnings:     make(map[string]string),
	}

	// Back navigation: if go_back is set, re-render the target step without validation.
	// MINOR-1 fix: when going back to step 2, re-load the masked client secret from draft
	// so the user can see it was saved (and we avoid losing the encrypted value).
	if goBack := r.FormValue("go_back"); goBack != "" {
		var targetStep int
		fmt.Sscan(goBack, &targetStep)
		if targetStep >= 1 && targetStep <= 4 {
			if targetStep == 2 {
				// Re-read OIDC fields from draft so step 2 re-renders correctly.
				if encSec, found, _ := h.draftStore.LoadDraft(r.Context(), "oidc_client_secret"); found && encSec != "" {
					if dec, err := decryptAES256GCM(h.secret, encSec); err == nil {
						data.MaskedSecret = maskSecret(dec)
					}
				}
				if v, found, _ := h.draftStore.LoadDraft(r.Context(), "oidc_issuer"); found {
					data.OIDCIssuer = v
				}
				if v, found, _ := h.draftStore.LoadDraft(r.Context(), "oidc_client_id"); found {
					data.OIDCClientID = v
				}
			}
			data.Step = targetStep
			h.tmpl.render(w, "bootstrap", data)
			return
		}
	}

	// Parse current step (wizard has 4 steps: 1=Instance, 2=OIDC, 3=Claim Mapping, 4=User Import).
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
		// Show Step 3: Claim Mapping (pre-filled with Nebu defaults).
		data.Step = 3
		data.OIDCUserIDClaim = "name"
		data.OIDCDisplaynameClaim = "name"
		data.OIDCEmailClaim = "email"

	case 3:
		// Validate claim mapping fields (AC8 / AC1).
		oidcUserIDClaim := strings.TrimSpace(r.FormValue("oidc_user_id_claim"))
		oidcDisplaynameClaim := strings.TrimSpace(r.FormValue("oidc_displayname_claim"))
		oidcEmailClaim := strings.TrimSpace(r.FormValue("oidc_email_claim"))

		// Use the same validation regex as ClaimMappingHandler (defined in claim_mapping.go).
		validateClaimField(oidcUserIDClaim, "oidc_user_id_claim", data.Errors)
		validateClaimField(oidcDisplaynameClaim, "oidc_displayname_claim", data.Errors)
		validateClaimField(oidcEmailClaim, "oidc_email_claim", data.Errors)

		if len(data.Errors) > 0 {
			data.Step = 3
			data.OIDCUserIDClaim = oidcUserIDClaim
			data.OIDCDisplaynameClaim = oidcDisplaynameClaim
			data.OIDCEmailClaim = oidcEmailClaim
			w.WriteHeader(http.StatusUnprocessableEntity)
			h.tmpl.render(w, "bootstrap", data)
			return
		}
		// Save claim mapping to draft so ClaimSelectionHandler can pick it up.
		claimDraftFields := []struct{ key, value string }{
			{"oidc_user_id_claim", oidcUserIDClaim},
			{"oidc_displayname_claim", oidcDisplaynameClaim},
			{"oidc_email_claim", oidcEmailClaim},
		}
		for _, f := range claimDraftFields {
			if err := h.draftStore.SaveDraft(r.Context(), f.key, f.value); err != nil {
				slog.Error("failed to save draft", "step", 3, "key", f.key, "err", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}
		// Redirect to OIDC login — the callback will show claim selection.
		http.Redirect(w, r, "/admin/login/start?mode=bootstrap", http.StatusSeeOther)
		return

	case 4:
		// Step 4: User Import (Story 14-3b).
		// Handles two sub-actions:
		//   action=preview — fetch users from OIDC dir, render preview table.
		//   action=import  — re-fetch + call BulkImportUsers gRPC, render result.
		//   (no action)   — initial render of step 4.
		action := r.FormValue("action")
		data.Step = 4
		// Story 14-3c: OIDCDirectoryEnabled is true when either OIDC or SCIM is enabled.
		oidcEnabled4 := h.oidcFetcher != nil && h.oidcFetcher.IsEnabled()
		scimEnabled4 := h.scimFetcher != nil && h.scimFetcher.IsEnabled()
		data.OIDCDirectoryEnabled = oidcEnabled4 || scimEnabled4

		switch action {
		case "preview":
			// AC2: Fetch users and build preview table.
			// Story 14-3c (MINOR-2 fix): use the SCIM-preferred fetcher selection logic for preview
			// to avoid nil-dereference when oidcFetcher is nil but scimFetcher is enabled.
			if !data.OIDCDirectoryEnabled {
				// AC4: OIDC dir disabled — render with disabled state.
				h.tmpl.render(w, "bootstrap", data)
				return
			}
			if h.serverName == "" {
				slog.Error("step 4 preview: serverName is empty — Matrix User IDs will be malformed")
			}
			// SCIM takes priority over OIDC for preview (mirrors import action logic).
			var previewFetcher interface {
				FetchUsers(ctx context.Context) ([]OIDCDirectoryUser, error)
			}
			if h.scimFetcher != nil && h.scimFetcher.IsEnabled() {
				previewFetcher = h.scimFetcher
			} else if h.oidcFetcher != nil && h.oidcFetcher.IsEnabled() {
				previewFetcher = h.oidcFetcher
			}
			if previewFetcher == nil {
				data.ImportError = "Provider does not support user listing."
				h.tmpl.render(w, "bootstrap", data)
				return
			}
			oidcUsers, err := previewFetcher.FetchUsers(r.Context())
			if err != nil {
				slog.Error("step 4 preview: failed to fetch users", "err", err)
				data.ImportError = "Failed to fetch users from provider."
				h.tmpl.render(w, "bootstrap", data)
				return
			}
			preview := make([]ImportPreviewUser, 0, len(oidcUsers))
			for _, u := range oidcUsers {
				localpart := sanitizeOIDCSub(u.Sub)
				preview = append(preview, ImportPreviewUser{
					DisplayName:  u.DisplayName,
					Email:        u.Email,
					MatrixUserID: "@" + localpart + ":" + h.serverName,
				})
			}
			data.ImportPreview = preview

		case "import":
			// AC3 / HR-3: Singleton import lock — return HTTP 409 Conflict if an import is already running.
			if !importInProgress.CompareAndSwap(false, true) {
				http.Error(w, `{"error":"import already in progress"}`, http.StatusConflict)
				return
			}

			// Determine which fetcher to use: SCIM takes priority over OIDC (AC1, Story 14-3c).
			// A fetcher is "active" when it is non-nil and IsEnabled() returns true.
			var activeFetcher interface {
				FetchUsers(ctx context.Context) ([]OIDCDirectoryUser, error)
			}
			fetcherName := ""
			if h.scimFetcher != nil && h.scimFetcher.IsEnabled() {
				activeFetcher = h.scimFetcher
				fetcherName = "scim"
			} else if h.oidcFetcher != nil && h.oidcFetcher.IsEnabled() {
				activeFetcher = h.oidcFetcher
				fetcherName = "oidc"
			}

			if activeFetcher == nil {
				importInProgress.Store(false)
				data.ImportError = "Provider does not support user listing."
				h.tmpl.render(w, "bootstrap", data)
				return
			}
			if h.core == nil {
				importInProgress.Store(false)
				slog.Error("step 4 import: core gRPC client is nil")
				data.ImportError = "Import service unavailable."
				h.tmpl.render(w, "bootstrap", data)
				return
			}

			users, err := activeFetcher.FetchUsers(r.Context())
			if err != nil {
				importInProgress.Store(false)
				slog.Error("step 4 import: failed to fetch users", "fetcher", fetcherName, "err", err)
				data.ImportError = "Failed to fetch users from provider."
				h.tmpl.render(w, "bootstrap", data)
				return
			}

			// Initialise progress counters for the import run.
			importProgress.imported.Store(0)
			importProgress.total.Store(int32(len(users)))
			importProgress.failed.Store(0)
			importProgress.done.Store(false)

			// Build OIDCUserClaims for each user.
			userClaims := make([]*pb.OIDCUserClaims, 0, len(users))
			for _, u := range users {
				localpart := sanitizeOIDCSub(u.Sub)
				userClaims = append(userClaims, &pb.OIDCUserClaims{
					UserId:      "@" + localpart + ":" + h.serverName,
					// SystemRole is intentionally absent: Core hard-codes "user" for all
					// bulk-imported accounts (SEC Gate 2 F-2 — field removed from proto).
					DisplayName: u.DisplayName,
					Email:       u.Email,
				})
			}
			resp, err := h.core.BulkImportUsers(r.Context(), &pb.BulkImportUsersRequest{Users: userClaims})
			if err != nil {
				importProgress.done.Store(true)
				importInProgress.Store(false)
				slog.Error("step 4 import: BulkImportUsers gRPC failed", "err", err)
				data.ImportError = "Import request to core failed. Please try again."
				h.tmpl.render(w, "bootstrap", data)
				return
			}

			// Update final counters from the gRPC response.
			importProgress.imported.Store(resp.GetImported())
			importProgress.failed.Store(resp.GetFailed())
			importProgress.done.Store(true)
			importInProgress.Store(false)

			data.ImportResult = &ImportResult{
				Imported: resp.GetImported(),
				Skipped:  resp.GetSkipped(),
				Failed:   resp.GetFailed(),
			}
		}
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
