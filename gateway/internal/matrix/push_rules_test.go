package matrix

// ─── Story 7-30: Push Rules API — GET/PUT/DELETE /pushrules + Pushers ────────
//
// Tests written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file MUST FAIL until push_rules.go is created and
// routes are registered in main.go.
//
// Acceptance Criteria covered:
//   AC1  — GET /pushrules/ → 200 with full ruleset grouped under {"global":{...}}
//   AC2  — Default rules seeded lazily on first GET; idempotent on second call
//   AC3  — GET /pushrules/{scope}/{kind}/{ruleId} → 200 single rule or 404 M_NOT_FOUND
//   AC4  — PUT creates/overwrites custom rule; overwriting default rule → 400 M_INVALID_PARAM
//   AC5  — DELETE removes custom rule; deleting default rule → 400 M_INVALID_PARAM
//   AC6  — PUT /{ruleId}/enabled toggles enabled on any rule (including defaults)
//   AC7  — PUT /{ruleId}/actions replaces actions on any rule (including defaults)
//   AC8  — Scope != "global" → 400 M_INVALID_PARAM
//   AC9  — GET /pushers → 200 with {"pushers":[]} when none registered
//   AC10 — POST /pushers/set with non-null kind registers; kind=null deregisters
//
// Design:
//   - PushRulesHandler takes a PushRulesDB interface (consumer-defined, ADR-009).
//   - PushersHandler takes a PushersDB interface.
//   - mockPushRulesDB implements PushRulesDB in-memory for deterministic tests.
//   - mockPushersDB implements PushersDB in-memory.
//   - buildAuthedPushRulesHandler wires JWTMiddleware → all push rule routes.
//   - buildAuthedPushersHandler wires JWTMiddleware → pusher routes.
//
// NOTE: PushRulesHandler, PushRulesConfig, NewPushRulesHandler, PushRulesDB,
// PushRule, PushRuleRow, PushersHandler, PushersConfig, NewPushersHandler,
// PushersDB, PusherRow are declared in gateway/internal/matrix/push_rules.go
// — which does NOT exist yet. Every test in this file MUST fail with a
// compilation error until push_rules.go is created.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/auth"
	"github.com/nebu/nebu/internal/middleware"
)

// ─── Mock PushRulesDB ─────────────────────────────────────────────────────────

// mockPushRulesDB implements PushRulesDB in memory.
// Each rule is keyed by "scope\nkind\nruleId".
type mockPushRulesDB struct {
	mu    sync.Mutex
	rules map[string]PushRuleRow
	// If getErr is non-nil, all reads return it.
	getErr error
	// If putErr is non-nil, all writes return it.
	putErr error
}

func newMockPushRulesDB() *mockPushRulesDB {
	return &mockPushRulesDB{rules: make(map[string]PushRuleRow)}
}

func (m *mockPushRulesDB) ruleKey(scope, kind, ruleID string) string {
	return scope + "\n" + kind + "\n" + ruleID
}

// SeedDefaultRules inserts the 15 Matrix-spec default rules if the user has none.
// Called idempotently on every GET /pushrules/ invocation.
func (m *mockPushRulesDB) SeedDefaultRules(ctx context.Context, userID string) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	defaultRuleIDs := []struct {
		kind   string
		ruleID string
	}{
		{"override", "m.rule.master"},
		{"override", "m.rule.suppress_notices"},
		{"override", "m.rule.invite_for_me"},
		{"override", "m.rule.member_event"},
		{"override", "m.rule.is_user_mention"},
		{"override", "m.rule.contains_display_name"},
		{"override", "m.rule.is_room_mention"},
		{"override", "m.rule.tombstone"},
		{"override", "m.rule.roomnotif"},
		{"content", "m.rule.contains_user_name"},
		{"underride", "m.rule.call"},
		{"underride", "m.rule.encrypted_room_one_to_one"},
		{"underride", "m.rule.room_one_to_one"},
		{"underride", "m.rule.message"},
		{"underride", "m.rule.encrypted"},
	}

	for i, dr := range defaultRuleIDs {
		key := m.ruleKey("global", dr.kind, dr.ruleID)
		if _, exists := m.rules[key]; !exists {
			m.rules[key] = PushRuleRow{
				UserID:      userID,
				Scope:       "global",
				Kind:        dr.kind,
				RuleID:      dr.ruleID,
				Priority:    i,
				Enabled:     true,
				Conditions:  json.RawMessage(`[]`),
				Actions:     json.RawMessage(`["notify"]`),
				DefaultRule: true,
			}
		}
	}
	return nil
}

// GetAllRules returns all rules for userID, grouped by kind.
func (m *mockPushRulesDB) GetAllRules(ctx context.Context, userID, scope string) ([]PushRuleRow, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var rows []PushRuleRow
	for _, row := range m.rules {
		if row.UserID == userID && row.Scope == scope {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// GetRule returns a single rule or ErrPushRuleNotFound.
func (m *mockPushRulesDB) GetRule(ctx context.Context, userID, scope, kind, ruleID string) (PushRuleRow, error) {
	if m.getErr != nil {
		return PushRuleRow{}, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rules[m.ruleKey(scope, kind, ruleID)]
	if !ok || row.UserID != userID {
		return PushRuleRow{}, ErrPushRuleNotFound
	}
	return row, nil
}

// PutRule creates or replaces a custom rule (must not be a default rule).
func (m *mockPushRulesDB) PutRule(ctx context.Context, userID string, row PushRuleRow) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.ruleKey(row.Scope, row.Kind, row.RuleID)
	if existing, ok := m.rules[key]; ok && existing.UserID == userID && existing.DefaultRule {
		return ErrDefaultRuleImmutable
	}
	row.UserID = userID
	m.rules[key] = row
	return nil
}

// DeleteRule removes a custom rule. Returns ErrDefaultRuleImmutable for default rules.
func (m *mockPushRulesDB) DeleteRule(ctx context.Context, userID, scope, kind, ruleID string) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.ruleKey(scope, kind, ruleID)
	existing, ok := m.rules[key]
	if !ok || existing.UserID != userID {
		return ErrPushRuleNotFound
	}
	if existing.DefaultRule {
		return ErrDefaultRuleImmutable
	}
	delete(m.rules, key)
	return nil
}

// SetRuleEnabled updates the enabled flag of any rule (including defaults).
func (m *mockPushRulesDB) SetRuleEnabled(ctx context.Context, userID, scope, kind, ruleID string, enabled bool) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.ruleKey(scope, kind, ruleID)
	row, ok := m.rules[key]
	if !ok || row.UserID != userID {
		return ErrPushRuleNotFound
	}
	row.Enabled = enabled
	m.rules[key] = row
	return nil
}

// SetRuleActions updates the actions array of any rule (including defaults).
func (m *mockPushRulesDB) SetRuleActions(ctx context.Context, userID, scope, kind, ruleID string, actions json.RawMessage) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.ruleKey(scope, kind, ruleID)
	row, ok := m.rules[key]
	if !ok || row.UserID != userID {
		return ErrPushRuleNotFound
	}
	row.Actions = actions
	m.rules[key] = row
	return nil
}

// ─── Mock PushersDB ───────────────────────────────────────────────────────────

// mockPushersDB implements PushersDB in memory.
// Pushers are keyed by "userID\nappId\npushkey".
type mockPushersDB struct {
	mu      sync.Mutex
	pushers map[string]PusherRow
	getErr  error
	putErr  error
}

func newMockPushersDB() *mockPushersDB {
	return &mockPushersDB{pushers: make(map[string]PusherRow)}
}

func (m *mockPushersDB) pusherKey(userID, appID, pushkey string) string {
	return userID + "\n" + appID + "\n" + pushkey
}

// GetPushers returns all pushers for the given user.
func (m *mockPushersDB) GetPushers(ctx context.Context, userID string) ([]PusherRow, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var rows []PusherRow
	for _, p := range m.pushers {
		if p.UserID == userID {
			rows = append(rows, p)
		}
	}
	return rows, nil
}

// SetPusher creates or updates a pusher (upsert by userID+appID+pushkey).
func (m *mockPushersDB) SetPusher(ctx context.Context, p PusherRow) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pushers[m.pusherKey(p.UserID, p.AppID, p.Pushkey)] = p
	return nil
}

// DeletePusher removes the pusher identified by (userID, appID, pushkey).
func (m *mockPushersDB) DeletePusher(ctx context.Context, userID, appID, pushkey string) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pushers, m.pusherKey(userID, appID, pushkey))
	return nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// buildAuthedPushRulesHandler wires JWTMiddleware → PushRulesHandler and registers
// all six push rule routes on a mux. Returns (mux, makeToken).
//
// JWT sub is "test-sub-123" → user_id "@test-sub-123:test.local".
func buildAuthedPushRulesHandler(t *testing.T, db *mockPushRulesDB) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)
	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil, "test.local")

	handler := NewPushRulesHandler(PushRulesConfig{DB: db})

	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/client/v3/pushrules/",
		jwtMiddleware(http.HandlerFunc(handler.GetAllPushRules)))
	mux.Handle("GET /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}",
		jwtMiddleware(http.HandlerFunc(handler.GetPushRule)))
	mux.Handle("PUT /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}",
		jwtMiddleware(http.HandlerFunc(handler.PutPushRule)))
	mux.Handle("DELETE /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}",
		jwtMiddleware(http.HandlerFunc(handler.DeletePushRule)))
	mux.Handle("PUT /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}/enabled",
		jwtMiddleware(http.HandlerFunc(handler.PutPushRuleEnabled)))
	mux.Handle("PUT /_matrix/client/v3/pushrules/{scope}/{kind}/{ruleId}/actions",
		jwtMiddleware(http.HandlerFunc(handler.PutPushRuleActions)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// buildAuthedPushersHandler wires JWTMiddleware → PushersHandler on a mux.
func buildAuthedPushersHandler(t *testing.T, db *mockPushersDB) (http.Handler, func() string) {
	t.Helper()

	oidcSrv, privateKey := setupOIDCServer(t)
	t.Cleanup(oidcSrv.Close)

	provider := auth.NewProvider(context.Background(), oidcSrv.URL)
	jwtMiddleware := middleware.JWTMiddleware(provider, "nebu-gateway", "nebu_role", nil, nil, "test.local")

	handler := NewPushersHandler(PushersConfig{DB: db})

	mux := http.NewServeMux()
	mux.Handle("GET /_matrix/client/v3/pushers",
		jwtMiddleware(http.HandlerFunc(handler.GetPushers)))
	mux.Handle("POST /_matrix/client/v3/pushers/set",
		jwtMiddleware(http.HandlerFunc(handler.SetPusher)))

	makeToken := func() string {
		return signJWT(t, oidcSrv.URL, privateKey, time.Now().Add(time.Hour), nil)
	}

	return mux, makeToken
}

// seedDefaults calls GET /pushrules/ to trigger lazy seeding in the mock, then
// returns the userID so tests can directly set rules in the DB.
const pushRulesAuthUserID = "@test-sub-123:test.local"

// ─── AC1: GET /pushrules/ returns 200 with grouped ruleset ───────────────────

func TestGetAllPushRules_ReturnsGlobalRuleset(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Body must be a JSON object with "global" key.
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v; body: %s", err, w.Body.String())
	}
	if _, ok := resp["global"]; !ok {
		t.Errorf("expected 'global' key in response, got keys: %v; body: %s", keysOf(resp), w.Body.String())
	}
}

// ─── AC1 + AC2: global.override contains m.rule.master ───────────────────────

func TestGetAllPushRules_ContainsMRuleMaster(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Parse the nested structure: {"global":{"override":[{"rule_id":"m.rule.master",...}],...}}
	var resp struct {
		Global map[string][]struct {
			RuleID string `json:"rule_id"`
		} `json:"global"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response parse error: %v; body: %s", err, w.Body.String())
	}

	overrides := resp.Global["override"]
	if len(overrides) == 0 {
		t.Fatalf("expected override rules, got none; body: %s", w.Body.String())
	}

	found := false
	for _, r := range overrides {
		if r.RuleID == "m.rule.master" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected m.rule.master in global.override; body: %s", w.Body.String())
	}
}

// ─── AC2: Lazy seeding is idempotent — second GET produces no duplicate rows ──

func TestGetAllPushRules_LazySeeding_Idempotent(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)
	token := makeToken()

	// First call — seeds defaults.
	req1 := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/", nil)
	req1.Header.Set("Authorization", "Bearer "+token)
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first GET: expected 200, got %d; body: %s", w1.Code, w1.Body.String())
	}

	countAfterFirst := len(db.rules)

	// Second call — must not add duplicate rows.
	req2 := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second GET: expected 200, got %d; body: %s", w2.Code, w2.Body.String())
	}

	countAfterSecond := len(db.rules)
	if countAfterFirst != countAfterSecond {
		t.Errorf("idempotency violated: rule count changed from %d to %d after second GET",
			countAfterFirst, countAfterSecond)
	}
}

// ─── AC3: GET single rule — found ─────────────────────────────────────────────

func TestGetPushRule_ExistingRule_Returns200(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)
	token := makeToken()

	// Seed defaults by calling GET /pushrules/.
	seedReq := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/", nil)
	seedReq.Header.Set("Authorization", "Bearer "+token)
	seedW := httptest.NewRecorder()
	mux.ServeHTTP(seedW, seedReq)
	if seedW.Code != http.StatusOK {
		t.Fatalf("seed: expected 200, got %d; body: %s", seedW.Code, seedW.Body.String())
	}

	// Now fetch the single rule.
	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/global/override/m.rule.master", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var rule struct {
		RuleID  string          `json:"rule_id"`
		Enabled bool            `json:"enabled"`
		Actions json.RawMessage `json:"actions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rule); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if rule.RuleID != "m.rule.master" {
		t.Errorf("expected rule_id 'm.rule.master', got %q", rule.RuleID)
	}
}

// ─── AC3: GET single rule — not found → 404 M_NOT_FOUND ──────────────────────

func TestGetPushRule_NonExistent_Returns404(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/global/override/nonexistent.rule", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_NOT_FOUND" {
		t.Errorf("expected errcode M_NOT_FOUND, got %q", errResp["errcode"])
	}
}

// ─── AC4: PUT creates a custom rule ───────────────────────────────────────────

func TestPutPushRule_CreatesCustomRule(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)
	token := makeToken()

	body := `{"conditions":[],"actions":["notify"]}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/pushrules/global/override/my.rule.test",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT: expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify the rule can be retrieved.
	getReq := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/pushrules/global/override/my.rule.test", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET after PUT: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}
	if !strings.Contains(getW.Body.String(), "my.rule.test") {
		t.Errorf("expected rule_id 'my.rule.test' in GET response; body: %s", getW.Body.String())
	}
}

// ─── AC4: PUT to overwrite a default rule returns 400 M_INVALID_PARAM ─────────

func TestPutPushRule_DefaultRule_Returns400(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)
	token := makeToken()

	// Seed defaults.
	seedReq := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/", nil)
	seedReq.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(httptest.NewRecorder(), seedReq)

	body := `{"conditions":[],"actions":["notify"]}`
	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/pushrules/global/override/m.rule.master",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %q", errResp["errcode"])
	}
}

// ─── AC5: DELETE custom rule succeeds; subsequent GET returns 404 ──────────────

func TestDeletePushRule_CustomRule_Succeeds(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)
	token := makeToken()

	// Create the custom rule first.
	putReq := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/pushrules/global/override/my.rule.test",
		bytes.NewBufferString(`{"conditions":[],"actions":["notify"]}`))
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/json")
	putW := httptest.NewRecorder()
	mux.ServeHTTP(putW, putReq)
	if putW.Code != http.StatusOK {
		t.Fatalf("setup PUT: expected 200, got %d; body: %s", putW.Code, putW.Body.String())
	}

	// Delete the rule.
	delReq := httptest.NewRequest(http.MethodDelete,
		"/_matrix/client/v3/pushrules/global/override/my.rule.test", nil)
	delReq.Header.Set("Authorization", "Bearer "+token)
	delW := httptest.NewRecorder()
	mux.ServeHTTP(delW, delReq)

	if delW.Code != http.StatusOK {
		t.Fatalf("DELETE: expected 200, got %d; body: %s", delW.Code, delW.Body.String())
	}

	// Subsequent GET must return 404.
	getReq := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/pushrules/global/override/my.rule.test", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE: expected 404, got %d; body: %s", getW.Code, getW.Body.String())
	}
}

// ─── AC5: DELETE default rule returns 400 M_INVALID_PARAM ────────────────────

func TestDeletePushRule_DefaultRule_Returns400(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)
	token := makeToken()

	// Seed defaults.
	seedReq := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/", nil)
	seedReq.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(httptest.NewRecorder(), seedReq)

	req := httptest.NewRequest(http.MethodDelete,
		"/_matrix/client/v3/pushrules/global/override/m.rule.master", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_INVALID_PARAM" {
		t.Errorf("expected errcode M_INVALID_PARAM, got %q", errResp["errcode"])
	}
}

// ─── AC6: PUT /enabled toggles enabled flag on any rule (including defaults) ──

func TestPutPushRuleEnabled_ToggleDefaultRule(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)
	token := makeToken()

	// Seed defaults.
	seedReq := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/", nil)
	seedReq.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(httptest.NewRecorder(), seedReq)

	// Disable m.rule.master.
	enableReq := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/pushrules/global/override/m.rule.master/enabled",
		bytes.NewBufferString(`{"enabled":false}`))
	enableReq.Header.Set("Authorization", "Bearer "+token)
	enableReq.Header.Set("Content-Type", "application/json")
	enableW := httptest.NewRecorder()
	mux.ServeHTTP(enableW, enableReq)

	if enableW.Code != http.StatusOK {
		t.Fatalf("PUT /enabled: expected 200, got %d; body: %s", enableW.Code, enableW.Body.String())
	}

	// Verify the rule now has enabled=false.
	getReq := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/pushrules/global/override/m.rule.master", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET after /enabled PUT: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}

	var rule struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &rule); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, getW.Body.String())
	}
	if rule.Enabled {
		t.Errorf("expected enabled=false after PUT /enabled with {\"enabled\":false}, got enabled=true; body: %s", getW.Body.String())
	}
}

// ─── AC6: PUT /enabled on non-existent rule returns 404 ──────────────────────

func TestPutPushRuleEnabled_NonExistentRule_Returns404(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)

	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/pushrules/global/override/no.such.rule/enabled",
		bytes.NewBufferString(`{"enabled":false}`))
	req.Header.Set("Authorization", "Bearer "+makeToken())
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", w.Code, w.Body.String())
	}
}

// ─── AC7: PUT /actions replaces actions array ──────────────────────────────────

func TestPutPushRuleActions_UpdatesActions(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)
	token := makeToken()

	// Create the custom rule.
	putReq := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/pushrules/global/override/my.rule.test",
		bytes.NewBufferString(`{"conditions":[],"actions":["notify"]}`))
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(httptest.NewRecorder(), putReq)

	// Update actions.
	actionsReq := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/pushrules/global/override/my.rule.test/actions",
		bytes.NewBufferString(`{"actions":["dont_notify"]}`))
	actionsReq.Header.Set("Authorization", "Bearer "+token)
	actionsReq.Header.Set("Content-Type", "application/json")
	actionsW := httptest.NewRecorder()
	mux.ServeHTTP(actionsW, actionsReq)

	if actionsW.Code != http.StatusOK {
		t.Fatalf("PUT /actions: expected 200, got %d; body: %s", actionsW.Code, actionsW.Body.String())
	}

	// Verify the rule now has updated actions.
	getReq := httptest.NewRequest(http.MethodGet,
		"/_matrix/client/v3/pushrules/global/override/my.rule.test", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET after /actions PUT: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}
	if !strings.Contains(getW.Body.String(), "dont_notify") {
		t.Errorf("expected 'dont_notify' in actions after PUT /actions; body: %s", getW.Body.String())
	}
}

// ─── AC8: Scope != "global" returns 400 M_INVALID_PARAM ──────────────────────

func TestPushRule_InvalidScope_Returns400(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/_matrix/client/v3/pushrules/device/override/m.rule.master", ""},
		{http.MethodPut, "/_matrix/client/v3/pushrules/device/override/my.rule", `{"conditions":[],"actions":["notify"]}`},
		{http.MethodDelete, "/_matrix/client/v3/pushrules/device/override/my.rule", ""},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s %s", tc.method, tc.path), func(t *testing.T) {
			var bodyReader *bytes.Reader
			if tc.body != "" {
				bodyReader = bytes.NewReader([]byte(tc.body))
			} else {
				bodyReader = bytes.NewReader(nil)
			}
			req := httptest.NewRequest(tc.method, tc.path, bodyReader)
			req.Header.Set("Authorization", "Bearer "+makeToken())
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d; body: %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "M_INVALID_PARAM") {
				t.Errorf("expected M_INVALID_PARAM in body; body: %s", w.Body.String())
			}
		})
	}
}

// ─── AC8: Bad JSON body on PUT returns 400 M_BAD_JSON ────────────────────────

func TestPutPushRule_BadJSON_Returns400(t *testing.T) {
	db := newMockPushRulesDB()
	mux, makeToken := buildAuthedPushRulesHandler(t, db)

	req := httptest.NewRequest(http.MethodPut,
		"/_matrix/client/v3/pushrules/global/override/my.rule.test",
		bytes.NewBufferString(`{not valid json`))
	req.Header.Set("Authorization", "Bearer "+makeToken())
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_BAD_JSON" {
		t.Errorf("expected errcode M_BAD_JSON, got %q", errResp["errcode"])
	}
}

// ─── Unauthenticated push rules request returns 401 ──────────────────────────

func TestGetAllPushRules_Unauthenticated_Returns401(t *testing.T) {
	db := newMockPushRulesDB()
	mux, _ := buildAuthedPushRulesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/", nil)
	// No Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_MISSING_TOKEN") {
		t.Errorf("expected M_MISSING_TOKEN in body; body: %s", w.Body.String())
	}
}

// ─── AC9: GET /pushers returns {"pushers":[]} when empty ─────────────────────

func TestGetPushers_EmptyList_Returns200WithEmptyArray(t *testing.T) {
	db := newMockPushersDB()
	mux, makeToken := buildAuthedPushersHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushers", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Pushers []json.RawMessage `json:"pushers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if resp.Pushers == nil || len(resp.Pushers) != 0 {
		t.Errorf("expected empty pushers array, got %v; body: %s", resp.Pushers, w.Body.String())
	}
	// Must not be null.
	if strings.Contains(w.Body.String(), `"pushers":null`) {
		t.Errorf("'pushers' must not be null; body: %s", w.Body.String())
	}
}

// ─── AC10: POST /pushers/set registers a pusher ───────────────────────────────

func TestSetPusher_RegisterPusher_AppearsInGetPushers(t *testing.T) {
	db := newMockPushersDB()
	mux, makeToken := buildAuthedPushersHandler(t, db)
	token := makeToken()

	pusherBody := `{"pushkey":"pk1","kind":"http","app_id":"app1","app_display_name":"Test","device_display_name":"Phone","lang":"en","data":{"url":"https://example.com/push"}}`
	postReq := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/pushers/set",
		bytes.NewBufferString(pusherBody))
	postReq.Header.Set("Authorization", "Bearer "+token)
	postReq.Header.Set("Content-Type", "application/json")
	postW := httptest.NewRecorder()
	mux.ServeHTTP(postW, postReq)

	if postW.Code != http.StatusOK {
		t.Fatalf("POST /pushers/set: expected 200, got %d; body: %s", postW.Code, postW.Body.String())
	}

	// GET /pushers must now return 1 pusher.
	getReq := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushers", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET /pushers: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}

	var resp struct {
		Pushers []json.RawMessage `json:"pushers"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, getW.Body.String())
	}
	if len(resp.Pushers) != 1 {
		t.Errorf("expected 1 pusher after registration, got %d; body: %s", len(resp.Pushers), getW.Body.String())
	}
}

// ─── AC10: POST /pushers/set with kind=null deregisters pusher ───────────────

func TestSetPusher_KindNull_DeregistersPusher(t *testing.T) {
	db := newMockPushersDB()
	mux, makeToken := buildAuthedPushersHandler(t, db)
	token := makeToken()

	// Register first.
	regBody := `{"pushkey":"pk1","kind":"http","app_id":"app1","app_display_name":"Test","device_display_name":"Phone","lang":"en","data":{"url":"https://example.com/push"}}`
	regReq := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/pushers/set",
		bytes.NewBufferString(regBody))
	regReq.Header.Set("Authorization", "Bearer "+token)
	regReq.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(httptest.NewRecorder(), regReq)

	// Deregister with kind=null.
	deregBody := `{"pushkey":"pk1","kind":null,"app_id":"app1"}`
	deregReq := httptest.NewRequest(http.MethodPost, "/_matrix/client/v3/pushers/set",
		bytes.NewBufferString(deregBody))
	deregReq.Header.Set("Authorization", "Bearer "+token)
	deregReq.Header.Set("Content-Type", "application/json")
	deregW := httptest.NewRecorder()
	mux.ServeHTTP(deregW, deregReq)

	if deregW.Code != http.StatusOK {
		t.Fatalf("POST /pushers/set (deregister): expected 200, got %d; body: %s", deregW.Code, deregW.Body.String())
	}

	// GET /pushers must now be empty.
	getReq := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushers", nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	if getW.Code != http.StatusOK {
		t.Fatalf("GET /pushers: expected 200, got %d; body: %s", getW.Code, getW.Body.String())
	}

	var resp struct {
		Pushers []json.RawMessage `json:"pushers"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, getW.Body.String())
	}
	if len(resp.Pushers) != 0 {
		t.Errorf("expected empty pushers after deregistration, got %d; body: %s", len(resp.Pushers), getW.Body.String())
	}
}

// ─── AC9/AC10: Unauthenticated pusher requests return 401 ────────────────────

func TestGetPushers_Unauthenticated_Returns401(t *testing.T) {
	db := newMockPushersDB()
	mux, _ := buildAuthedPushersHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushers", nil)
	// No Authorization header.

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "M_MISSING_TOKEN") {
		t.Errorf("expected M_MISSING_TOKEN in body; body: %s", w.Body.String())
	}
}

// ─── DB error path → 500 M_UNKNOWN ───────────────────────────────────────────

func TestGetAllPushRules_DBError_Returns500(t *testing.T) {
	db := newMockPushRulesDB()
	db.getErr = fmt.Errorf("simulated DB failure")
	mux, makeToken := buildAuthedPushRulesHandler(t, db)

	req := httptest.NewRequest(http.MethodGet, "/_matrix/client/v3/pushrules/", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken())

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", w.Code, w.Body.String())
	}

	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("parse error: %v; body: %s", err, w.Body.String())
	}
	if errResp["errcode"] != "M_UNKNOWN" {
		t.Errorf("expected errcode M_UNKNOWN, got %q", errResp["errcode"])
	}
}

// ─── Utility ──────────────────────────────────────────────────────────────────

// keysOf returns the keys of a map as a slice (for diagnostic messages).
func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
