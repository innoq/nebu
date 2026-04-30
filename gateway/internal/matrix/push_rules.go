package matrix

// ─── Story 7-30: Push Rules API — GET/PUT/DELETE /pushrules + Pushers ────────
//
// Implements the Matrix push rules API:
//
//	GET    /_matrix/client/v3/pushrules/
//	GET    /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}
//	PUT    /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}
//	DELETE /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}
//	PUT    /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}/enabled
//	PUT    /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}/actions
//	GET    /_matrix/client/v3/pushers
//	POST   /_matrix/client/v3/pushers/set
//
// Push rules are stored directly in PostgreSQL — no gRPC to Elixir Core.
// Default rules are seeded lazily on first GET /pushrules/ (idempotent).
// Only scope "global" is supported; any other scope → 400 M_INVALID_PARAM.
//
// JWT required — jwtMiddleware enforces this before the handler is reached.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/nebu/nebu/internal/middleware"
)

// ─── Sentinel errors ─────────────────────────────────────────────────────────

// ErrPushRuleNotFound is returned by PushRulesDB when the requested rule does not exist.
var ErrPushRuleNotFound = errors.New("push rule not found")

// ErrDefaultRuleImmutable is returned when a caller tries to overwrite or delete a default rule.
var ErrDefaultRuleImmutable = errors.New("cannot modify default push rule")

// ─── Domain types ─────────────────────────────────────────────────────────────

// PushRuleRow is the raw row representation from the push_rules table.
type PushRuleRow struct {
	UserID      string
	Scope       string
	Kind        string
	RuleID      string
	Priority    int
	Enabled     bool
	Conditions  json.RawMessage // JSONB stored as []
	Actions     json.RawMessage // JSONB stored as ["notify"] etc.
	DefaultRule bool
}

// PusherRow is the raw row representation from the pushers table.
type PusherRow struct {
	UserID            string
	Pushkey           string
	Kind              string
	AppID             string
	AppDisplayName    string
	DeviceDisplayName string
	Lang              string
	Data              json.RawMessage // JSONB
}

// ─── Consumer-defined interfaces (ADR-009) ────────────────────────────────────

// PushRulesDB is the consumer-defined interface for reading and writing push rule rows.
type PushRulesDB interface {
	// SeedDefaultRules inserts the 15 Matrix-spec default rules for the given user
	// if they do not already exist. Safe to call multiple times (idempotent).
	SeedDefaultRules(ctx context.Context, userID string) error

	// GetAllRules returns all push rules for userID in the given scope, sorted by priority.
	GetAllRules(ctx context.Context, userID, scope string) ([]PushRuleRow, error)

	// GetRule returns a single rule or ErrPushRuleNotFound.
	GetRule(ctx context.Context, userID, scope, kind, ruleID string) (PushRuleRow, error)

	// PutRule creates or replaces a custom rule.
	// Returns ErrDefaultRuleImmutable if the rule already exists and is a default rule.
	PutRule(ctx context.Context, userID string, row PushRuleRow) error

	// DeleteRule removes a custom rule.
	// Returns ErrDefaultRuleImmutable for default rules, ErrPushRuleNotFound if absent.
	DeleteRule(ctx context.Context, userID, scope, kind, ruleID string) error

	// SetRuleEnabled updates the enabled flag of any rule (including defaults).
	// Returns ErrPushRuleNotFound if the rule does not exist.
	SetRuleEnabled(ctx context.Context, userID, scope, kind, ruleID string, enabled bool) error

	// SetRuleActions replaces the actions array of any rule (including defaults).
	// Returns ErrPushRuleNotFound if the rule does not exist.
	SetRuleActions(ctx context.Context, userID, scope, kind, ruleID string, actions json.RawMessage) error
}

// PushersDB is the consumer-defined interface for reading and writing pushers.
type PushersDB interface {
	// GetPushers returns all pushers registered for the given user.
	GetPushers(ctx context.Context, userID string) ([]PusherRow, error)

	// SetPusher creates or updates a pusher (upsert by userID+appID+pushkey).
	SetPusher(ctx context.Context, p PusherRow) error

	// DeletePusher removes the pusher identified by (userID, appID, pushkey).
	DeletePusher(ctx context.Context, userID, appID, pushkey string) error
}

// ─── PushRulesHandler ────────────────────────────────────────────────────────

// PushRulesConfig holds dependencies for NewPushRulesHandler.
type PushRulesConfig struct {
	DB PushRulesDB
}

// PushRulesHandler handles all /_matrix/client/v3/pushrules/* endpoints.
type PushRulesHandler struct {
	db PushRulesDB
}

// NewPushRulesHandler constructs a PushRulesHandler from the provided config.
func NewPushRulesHandler(cfg PushRulesConfig) *PushRulesHandler {
	return &PushRulesHandler{db: cfg.DB}
}

// ─── Wire types ───────────────────────────────────────────────────────────────

// pushRuleWire is the JSON shape of a single rule in API responses.
type pushRuleWire struct {
	RuleID     string          `json:"rule_id"`
	Default    bool            `json:"default"`
	Enabled    bool            `json:"enabled"`
	Conditions json.RawMessage `json:"conditions"`
	Actions    json.RawMessage `json:"actions"`
}

// pushRuleSetWire is the JSON shape for PUT /pushrules/{scope}/{kind}/{ruleId}.
type pushRuleSetWire struct {
	Conditions json.RawMessage `json:"conditions"`
	Actions    json.RawMessage `json:"actions"`
}

// pushRuleEnabledWire is the JSON shape for PUT /pushrules/{scope}/{kind}/{ruleId}/enabled.
type pushRuleEnabledWire struct {
	Enabled bool `json:"enabled"`
}

// pushRuleActionsWire is the JSON shape for PUT /pushrules/{scope}/{kind}/{ruleId}/actions.
type pushRuleActionsWire struct {
	Actions json.RawMessage `json:"actions"`
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// kindOrder defines the canonical order of rule kinds in the GET /pushrules/ response.
var kindOrder = []string{"override", "content", "room", "sender", "underride"}

// validateScope returns false and writes a 400 M_INVALID_PARAM response if scope != "global".
func validateScope(w http.ResponseWriter, scope string) bool {
	if scope != "global" {
		writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "scope must be 'global'")
		return false
	}
	return true
}

// rowToWire converts a PushRuleRow to the API wire shape.
func rowToWire(row PushRuleRow) pushRuleWire {
	conds := row.Conditions
	if conds == nil {
		conds = json.RawMessage(`[]`)
	}
	acts := row.Actions
	if acts == nil {
		acts = json.RawMessage(`["notify"]`)
	}
	return pushRuleWire{
		RuleID:     row.RuleID,
		Default:    row.DefaultRule,
		Enabled:    row.Enabled,
		Conditions: conds,
		Actions:    acts,
	}
}

// extractUserID extracts the authenticated user_id from the request context.
// Returns empty string if not set (should not happen after jwtMiddleware).
func extractUserID(r *http.Request) string {
	uid, _ := r.Context().Value(middleware.ContextKeyUserID).(string)
	return uid
}

// ─── GetAllPushRules — AC1, AC2 ──────────────────────────────────────────────

// GetAllPushRules handles GET /_matrix/client/v3/pushrules/.
//
// Flow:
//  1. Extract authenticated user_id from JWT context.
//  2. Seed default rules (idempotent — ON CONFLICT DO NOTHING in SQL).
//  3. Fetch all rules for the user under scope "global".
//  4. Group by kind, preserving canonical kind order.
//  5. Return 200 {"global":{"override":[...],"content":[...],...}}.
func (h *PushRulesHandler) GetAllPushRules(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Not authenticated")
		return
	}

	if err := h.db.SeedDefaultRules(r.Context(), userID); err != nil {
		slog.Error("SeedDefaultRules failed", "err", err, "user_id", userID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	rows, err := h.db.GetAllRules(r.Context(), userID, "global")
	if err != nil {
		slog.Error("GetAllRules failed", "err", err, "user_id", userID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	// Group rules by kind.
	grouped := make(map[string][]pushRuleWire)
	for _, kind := range kindOrder {
		grouped[kind] = []pushRuleWire{} // ensure all kinds are present (even if empty)
	}
	for _, row := range rows {
		grouped[row.Kind] = append(grouped[row.Kind], rowToWire(row))
	}

	resp := map[string]map[string][]pushRuleWire{
		"global": grouped,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ─── GetPushRule — AC3 ────────────────────────────────────────────────────────

// GetPushRule handles GET /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}.
func (h *PushRulesHandler) GetPushRule(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Not authenticated")
		return
	}

	scope := r.PathValue("scope")
	kind := r.PathValue("kind")
	ruleID := r.PathValue("ruleId")

	if !validateScope(w, scope) {
		return
	}

	row, err := h.db.GetRule(r.Context(), userID, scope, kind, ruleID)
	if err != nil {
		if errors.Is(err, ErrPushRuleNotFound) {
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Push rule not found")
			return
		}
		slog.Error("GetRule failed", "err", err, "user_id", userID, "rule_id", ruleID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rowToWire(row))
}

// ─── PutPushRule — AC4 ────────────────────────────────────────────────────────

// PutPushRule handles PUT /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}.
//
// Creates or replaces a custom rule. Returns 400 M_INVALID_PARAM if the rule is a default rule.
func (h *PushRulesHandler) PutPushRule(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Not authenticated")
		return
	}

	scope := r.PathValue("scope")
	kind := r.PathValue("kind")
	ruleID := r.PathValue("ruleId")

	if !validateScope(w, scope) {
		return
	}

	var body pushRuleSetWire
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Invalid JSON body")
		return
	}

	// Ensure non-null defaults for JSONB columns.
	if body.Conditions == nil {
		body.Conditions = json.RawMessage(`[]`)
	}
	if body.Actions == nil {
		body.Actions = json.RawMessage(`["notify"]`)
	}

	row := PushRuleRow{
		UserID:     userID,
		Scope:      scope,
		Kind:       kind,
		RuleID:     ruleID,
		Enabled:    true,
		Conditions: body.Conditions,
		Actions:    body.Actions,
	}

	if err := h.db.PutRule(r.Context(), userID, row); err != nil {
		if errors.Is(err, ErrDefaultRuleImmutable) {
			writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "Cannot overwrite a default rule")
			return
		}
		slog.Error("PutRule failed", "err", err, "user_id", userID, "rule_id", ruleID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// ─── DeletePushRule — AC5 ─────────────────────────────────────────────────────

// DeletePushRule handles DELETE /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}.
//
// Removes a custom rule. Returns 400 M_INVALID_PARAM for default rules,
// 404 M_NOT_FOUND if the rule does not exist.
func (h *PushRulesHandler) DeletePushRule(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Not authenticated")
		return
	}

	scope := r.PathValue("scope")
	kind := r.PathValue("kind")
	ruleID := r.PathValue("ruleId")

	if !validateScope(w, scope) {
		return
	}

	if err := h.db.DeleteRule(r.Context(), userID, scope, kind, ruleID); err != nil {
		if errors.Is(err, ErrDefaultRuleImmutable) {
			writeMatrixError(w, http.StatusBadRequest, "M_INVALID_PARAM", "Cannot delete a default rule")
			return
		}
		if errors.Is(err, ErrPushRuleNotFound) {
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Push rule not found")
			return
		}
		slog.Error("DeleteRule failed", "err", err, "user_id", userID, "rule_id", ruleID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// ─── PutPushRuleEnabled — AC6 ─────────────────────────────────────────────────

// PutPushRuleEnabled handles PUT /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}/enabled.
//
// Enables or disables any rule (including default rules).
func (h *PushRulesHandler) PutPushRuleEnabled(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Not authenticated")
		return
	}

	scope := r.PathValue("scope")
	kind := r.PathValue("kind")
	ruleID := r.PathValue("ruleId")

	if !validateScope(w, scope) {
		return
	}

	var body pushRuleEnabledWire
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Invalid JSON body")
		return
	}

	if err := h.db.SetRuleEnabled(r.Context(), userID, scope, kind, ruleID, body.Enabled); err != nil {
		if errors.Is(err, ErrPushRuleNotFound) {
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Push rule not found")
			return
		}
		slog.Error("SetRuleEnabled failed", "err", err, "user_id", userID, "rule_id", ruleID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// ─── PutPushRuleActions — AC7 ─────────────────────────────────────────────────

// PutPushRuleActions handles PUT /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}/actions.
//
// Replaces the actions array of any rule (including default rules).
func (h *PushRulesHandler) PutPushRuleActions(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Not authenticated")
		return
	}

	scope := r.PathValue("scope")
	kind := r.PathValue("kind")
	ruleID := r.PathValue("ruleId")

	if !validateScope(w, scope) {
		return
	}

	var body pushRuleActionsWire
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Invalid JSON body")
		return
	}

	if body.Actions == nil {
		body.Actions = json.RawMessage(`["notify"]`)
	}

	if err := h.db.SetRuleActions(r.Context(), userID, scope, kind, ruleID, body.Actions); err != nil {
		if errors.Is(err, ErrPushRuleNotFound) {
			writeMatrixError(w, http.StatusNotFound, "M_NOT_FOUND", "Push rule not found")
			return
		}
		slog.Error("SetRuleActions failed", "err", err, "user_id", userID, "rule_id", ruleID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// ─── PushersHandler ───────────────────────────────────────────────────────────

// PushersConfig holds dependencies for NewPushersHandler.
type PushersConfig struct {
	DB PushersDB
}

// PushersHandler handles GET /_matrix/client/v3/pushers and POST /pushers/set.
type PushersHandler struct {
	db PushersDB
}

// NewPushersHandler constructs a PushersHandler from the provided config.
func NewPushersHandler(cfg PushersConfig) *PushersHandler {
	return &PushersHandler{db: cfg.DB}
}

// pusherWire is the JSON shape of a pusher in API responses.
type pusherWire struct {
	UserID            string          `json:"user_id,omitempty"`
	Pushkey           string          `json:"pushkey"`
	Kind              string          `json:"kind"`
	AppID             string          `json:"app_id"`
	AppDisplayName    string          `json:"app_display_name"`
	DeviceDisplayName string          `json:"device_display_name"`
	Lang              string          `json:"lang"`
	Data              json.RawMessage `json:"data"`
}

// setPusherWire is the JSON body shape for POST /pushers/set.
// Kind is a pointer — null means deregister.
type setPusherWire struct {
	Pushkey           string          `json:"pushkey"`
	Kind              *string         `json:"kind"` // null → deregister
	AppID             string          `json:"app_id"`
	AppDisplayName    string          `json:"app_display_name"`
	DeviceDisplayName string          `json:"device_display_name"`
	Lang              string          `json:"lang"`
	Data              json.RawMessage `json:"data"`
}

// ─── GetPushers — AC9 ─────────────────────────────────────────────────────────

// GetPushers handles GET /_matrix/client/v3/pushers.
//
// Returns {"pushers":[...]} — empty array (never null) when none registered.
func (h *PushersHandler) GetPushers(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Not authenticated")
		return
	}

	rows, err := h.db.GetPushers(r.Context(), userID)
	if err != nil {
		slog.Error("GetPushers failed", "err", err, "user_id", userID)
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}

	pushers := make([]pusherWire, 0, len(rows))
	for _, row := range rows {
		data := row.Data
		if data == nil {
			data = json.RawMessage(`{}`)
		}
		pushers = append(pushers, pusherWire{
			Pushkey:           row.Pushkey,
			Kind:              row.Kind,
			AppID:             row.AppID,
			AppDisplayName:    row.AppDisplayName,
			DeviceDisplayName: row.DeviceDisplayName,
			Lang:              row.Lang,
			Data:              data,
		})
	}

	resp := struct {
		Pushers []pusherWire `json:"pushers"`
	}{
		Pushers: pushers,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ─── SetPusher — AC10 ─────────────────────────────────────────────────────────

// SetPusher handles POST /_matrix/client/v3/pushers/set.
//
// With non-null kind: registers or updates a pusher (upsert by userID+appID+pushkey).
// With null kind: deregisters the pusher identified by (appID, pushkey).
func (h *PushersHandler) SetPusher(w http.ResponseWriter, r *http.Request) {
	userID := extractUserID(r)
	if userID == "" {
		writeMatrixError(w, http.StatusUnauthorized, "M_MISSING_TOKEN", "Not authenticated")
		return
	}

	var body setPusherWire
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Invalid JSON body")
		return
	}

	// kind == null → deregister.
	if body.Kind == nil {
		if err := h.db.DeletePusher(r.Context(), userID, body.AppID, body.Pushkey); err != nil {
			slog.Error("DeletePusher failed", "err", err, "user_id", userID)
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
			return
		}
	} else {
		data := body.Data
		if data == nil {
			data = json.RawMessage(`{}`)
		}
		lang := body.Lang
		if lang == "" {
			lang = "en"
		}
		p := PusherRow{
			UserID:            userID,
			Pushkey:           body.Pushkey,
			Kind:              *body.Kind,
			AppID:             body.AppID,
			AppDisplayName:    body.AppDisplayName,
			DeviceDisplayName: body.DeviceDisplayName,
			Lang:              lang,
			Data:              data,
		}
		if err := h.db.SetPusher(r.Context(), p); err != nil {
			slog.Error("SetPusher failed", "err", err, "user_id", userID)
			writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}
