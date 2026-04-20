package admin

// Story 5.11: Transactional Bootstrap Completion — Acceptance Tests
//
// These tests encode Acceptance Criteria 1–5:
//   AC1: SaveBootstrapConfig, SaveAdminGroupClaim, and CompleteBootstrap run inside a single sql.Tx.
//   AC2: If CompleteBootstrap returns ErrAlreadyCompleted, the TX is rolled back — no server_config changes persist.
//   AC3: Rollback causes the handler to respond 403 Forbidden.
//   AC4: ClearDraft runs inside the same TX on the success path; its failure aborts the TX.
//   AC5: Unit test verifies server_config is unchanged when CompleteBootstrap fails.
//
// IMPLEMENTATION:
//   ClaimSelectionHandler uses a.runInTx — a function field injected at construction time.
//   Production wires a real sql.Tx; tests inject a fake that models transactional semantics
//   via pending state + commit()/rollback() methods.
//
//   txAwareConfigStore implements sqlQuerier so the package-level *Tx helper functions
//   (saveBootstrapConfigTx, saveAdminGroupClaimTx, clearDraftTx, completeBootstrapTx)
//   run against the fake without a real database.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeResult implements sql.Result for ExecContext returns from txAwareConfigStore.
type fakeResult struct {
	rowsAffected int64
}

func (f fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (f fakeResult) RowsAffected() (int64, error) { return f.rowsAffected, nil }

// txAwareConfigStore is a test double that implements:
//   - sqlQuerier (ExecContext + QueryRowContext) — so the *Tx helper functions can call it
//   - BootstrapDraftStore — for LoadDraft / SaveDraft / ClearDraft
//   - ServerConfigReader — for LoadOIDCConfig / LoadAdminGroupClaim
//
// Writes are staged in pending* state until commit() is called.
// rollback() discards all staged writes. ClearDraftCalled tracks whether
// clearDraftTx was invoked inside the TX.
type txAwareConfigStore struct {
	// committed state — what survives a commit
	committedIssuer string
	committedClaim  string

	// staged (pending) state — set during a TX, cleared on rollback
	pendingIssuer string
	pendingClaim  string

	// controls whether completeBootstrapTx should fail (0 rows affected) or succeed (1 row)
	completeBootstrapErr error

	// observable side-effects
	clearDraftCalled bool
	clearDraftErr    error

	// bootstrapAlreadyCompleted controls RowsAffected for the bootstrap_completed insert.
	// When true, ExecContext returns 0 rows to trigger ErrAlreadyCompleted in completeBootstrapTx.
	bootstrapAlreadyCompleted bool
}

// --- sqlQuerier interface ---

// ExecContext intercepts the SQL calls made by the *Tx helper functions and updates
// pending state accordingly. It does not execute real SQL.
func (s *txAwareConfigStore) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "bootstrap_draft"):
		// clearDraftTx: DELETE FROM bootstrap_draft
		s.clearDraftCalled = true
		if s.clearDraftErr != nil {
			return nil, s.clearDraftErr
		}
		return fakeResult{rowsAffected: 1}, nil

	case strings.Contains(query, "server_config"):
		// Determine which key is being written.
		// saveBootstrapConfigTx:   args = (key, value, set_at)
		// saveAdminGroupClaimTx:   SQL contains literal 'admin_group_claim', args = (value, set_at)
		// completeBootstrapTx:     args = ("bootstrap_completed", "true", set_at)
		if strings.Contains(query, "bootstrap_completed") || (len(args) >= 1 && args[0] == "bootstrap_completed") {
			// completeBootstrapTx
			if s.completeBootstrapErr != nil {
				return nil, s.completeBootstrapErr
			}
			if s.bootstrapAlreadyCompleted {
				return fakeResult{rowsAffected: 0}, nil // ON CONFLICT DO NOTHING → 0 rows
			}
			return fakeResult{rowsAffected: 1}, nil
		}
		if strings.Contains(query, "'admin_group_claim'") {
			// saveAdminGroupClaimTx: hardcoded key in SQL literal; args = (claimValue, set_at)
			if len(args) >= 1 {
				if v, ok := args[0].(string); ok {
					s.pendingClaim = v
				}
			}
			return fakeResult{rowsAffected: 1}, nil
		}
		// saveBootstrapConfigTx: args = (key, value, set_at)
		if len(args) >= 2 {
			key, _ := args[0].(string)
			val, _ := args[1].(string)
			if key == "oidc_issuer" {
				s.pendingIssuer = val
			}
		}
		return fakeResult{rowsAffected: 1}, nil
	}

	return fakeResult{rowsAffected: 1}, nil
}

// QueryRowContext is required by sqlQuerier. Not called by the *Tx helper functions.
func (s *txAwareConfigStore) QueryRowContext(_ context.Context, _ string, _ ...any) *sql.Row {
	return nil
}

// commit promotes pending state → committed state.
func (s *txAwareConfigStore) commit() {
	s.committedIssuer = s.pendingIssuer
	s.committedClaim = s.pendingClaim
}

// rollback discards all pending (staged) writes.
func (s *txAwareConfigStore) rollback() {
	s.pendingIssuer = ""
	s.pendingClaim = ""
}

// --- BootstrapDraftStore interface ---

func (s *txAwareConfigStore) SaveDraft(_ context.Context, _, _ string) error { return nil }

func (s *txAwareConfigStore) LoadDraft(_ context.Context, key string) (string, bool, error) {
	switch key {
	case "bootstrap_sub":
		return "test-sub", true, nil
	case "bootstrap_email":
		return "admin@example.com", true, nil
	case "instance_name":
		return "test-instance", true, nil
	case "oidc_issuer":
		return "https://attacker.com", true, nil
	case "oidc_client_id":
		return "attacker-client", true, nil
	case "oidc_client_secret":
		return "encrypted-attacker-secret", true, nil
	}
	return "", false, nil
}

// ClearDraft is NOT called directly by ClaimSelectionHandler any more — the handler
// uses clearDraftTx(ctx, q) inside runInTx. This method is kept for BootstrapDraftStore
// interface completeness but is not exercised in these tests.
func (s *txAwareConfigStore) ClearDraft(_ context.Context) error {
	return s.clearDraftErr
}

// --- ServerConfigReader interface ---

func (s *txAwareConfigStore) LoadOIDCConfig(_ context.Context) (string, string, string, error) {
	return s.committedIssuer, "test-client-id", "encrypted-secret", nil
}

func (s *txAwareConfigStore) SaveAdminGroupClaim(_ context.Context, _ string) error {
	return nil
}

func (s *txAwareConfigStore) LoadAdminGroupClaim(_ context.Context) (string, error) {
	return s.committedClaim, nil
}

func (s *txAwareConfigStore) CompleteBootstrap(_ context.Context) error {
	return nil
}

// newTestAdminAuthForTX builds an AdminAuth wired for ClaimSelectionHandler tests.
// runInTx is injected so that all four writes run through the same code path as production,
// but against the in-memory txAwareConfigStore instead of a real database.
func newTestAdminAuthForTX(t *testing.T, store *txAwareConfigStore) *AdminAuth {
	t.Helper()
	a := NewAdminAuth(nil, "test-client-id", "test-secret", "nebu_role", []byte("test-secret-key"), nil, nil)
	a.configReader = store
	a.draftStore = store

	// Inject runInTx: calls fn(store) using store as the sqlQuerier.
	// On success it commits (promotes pending → committed).
	// On error it rolls back (discards pending writes).
	a.runInTx = func(ctx context.Context, fn func(q sqlQuerier) error) error {
		err := fn(store)
		if err != nil {
			store.rollback()
			return err
		}
		store.commit()
		return nil
	}
	return a
}

// buildClaimSelectionRequest builds a POST /admin/bootstrap/select-claim request
// with a valid form body. ClaimSelectionHandler does not require a state cookie.
func buildClaimSelectionRequest(t *testing.T, claimValue string) *http.Request {
	t.Helper()
	body := "admin_group_claim=" + claimValue
	req := httptest.NewRequest("POST", "/admin/bootstrap/select-claim",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// ---------------------------------------------------------------------------
// AC2 + AC3 + AC5:
// TestClaimSelection_TXRollbackOnCompleteBootstrapFailure
//
// Given:  bootstrap already completed (completeBootstrapTx returns ErrAlreadyCompleted via
//         0 rows affected on ON CONFLICT DO NOTHING insert)
//         existing oidc_issuer = "https://old.example.com" in committed state
// When:   POST /admin/bootstrap/select-claim with oidc_issuer = "https://attacker.com"
//         (injected via draft store)
// Then:   (a) HTTP 403 Forbidden                              — AC3
//         (b) committedIssuer still "https://old.example.com" — AC2 / AC5
// ---------------------------------------------------------------------------
func TestClaimSelection_TXRollbackOnCompleteBootstrapFailure(t *testing.T) {
	store := &txAwareConfigStore{
		// Pre-existing committed state — must survive the failed attempt.
		committedIssuer: "https://old.example.com",
		committedClaim:  "instance_admin",

		// Simulate bootstrap_completed already in DB: ON CONFLICT DO NOTHING → 0 rows affected
		// → completeBootstrapTx returns ErrAlreadyCompleted.
		bootstrapAlreadyCompleted: true,
	}

	a := newTestAdminAuthForTX(t, store)
	req := buildClaimSelectionRequest(t, "instance_admin")
	rr := httptest.NewRecorder()
	a.ClaimSelectionHandler(rr, req)

	// AC3: handler must return 403 Forbidden when bootstrap already completed.
	if rr.Code != http.StatusForbidden {
		t.Errorf("AC3 FAIL: expected 403 Forbidden when bootstrap already completed, got %d (body: %q)",
			rr.Code, rr.Body.String())
	}

	// AC2 / AC5: committed state must be unchanged — the attacker's oidc_issuer must NOT persist.
	if store.committedIssuer != "https://old.example.com" {
		t.Errorf("AC2/AC5 FAIL: server_config oidc_issuer was overwritten despite TX failure.\n"+
			"  want: %q\n  got:  %q\n"+
			"  (TX rollback must discard pending writes when completeBootstrapTx fails)",
			"https://old.example.com", store.committedIssuer)
	}

	// The attacker's pending value must not have leaked into committed state.
	if store.committedIssuer == "https://attacker.com" {
		t.Errorf("AC2 FAIL: attacker oidc_issuer %q persisted in server_config after rollback",
			store.committedIssuer)
	}
}

// ---------------------------------------------------------------------------
// txAwareConfigStoreSuccess is a variant of txAwareConfigStore whose LoadDraft
// returns values suitable for the success path (non-attacker oidc_issuer).
// ---------------------------------------------------------------------------
type txAwareConfigStoreSuccess struct {
	txAwareConfigStore
}

func (s *txAwareConfigStoreSuccess) LoadDraft(_ context.Context, key string) (string, bool, error) {
	switch key {
	case "bootstrap_sub":
		return "test-sub", true, nil
	case "bootstrap_email":
		return "admin@example.com", true, nil
	case "instance_name":
		return "my-instance", true, nil
	case "oidc_issuer":
		return "https://new.example.com", true, nil
	case "oidc_client_id":
		return "nebu-admin", true, nil
	case "oidc_client_secret":
		return "encrypted-secret", true, nil
	}
	return "", false, nil
}

// newTestAdminAuthForTXSuccess builds an AdminAuth for success-path tests,
// using txAwareConfigStoreSuccess as both configReader and draftStore.
func newTestAdminAuthForTXSuccess(t *testing.T, store *txAwareConfigStoreSuccess) *AdminAuth {
	t.Helper()
	a := NewAdminAuth(nil, "test-client-id", "test-secret", "nebu_role", []byte("test-secret-key"), nil, nil)
	a.configReader = &store.txAwareConfigStore
	a.draftStore = store

	a.runInTx = func(ctx context.Context, fn func(q sqlQuerier) error) error {
		err := fn(&store.txAwareConfigStore)
		if err != nil {
			store.txAwareConfigStore.rollback()
			return err
		}
		store.txAwareConfigStore.commit()
		return nil
	}
	return a
}

// ---------------------------------------------------------------------------
// AC1 + AC4:
// TestClaimSelection_TXCommitsOnSuccess
//
// Given:  bootstrap_active=true (completeBootstrapTx succeeds — 1 row affected)
//         draft rows present (bootstrap_sub, oidc_issuer=https://new.example.com, etc.)
// When:   POST /admin/bootstrap/select-claim succeeds
// Then:   (a) all three writes (config, claim, completion) are visible in committed state — AC1
//         (b) draft was cleared inside the TX (clearDraftCalled=true)                      — AC4
//         (c) HTTP 303 SeeOther redirect to /admin/dashboard
// ---------------------------------------------------------------------------
func TestClaimSelection_TXCommitsOnSuccess(t *testing.T) {
	successStore := &txAwareConfigStoreSuccess{
		txAwareConfigStore: txAwareConfigStore{
			committedIssuer:           "",
			committedClaim:            "",
			completeBootstrapErr:      nil, // success path
			bootstrapAlreadyCompleted: false,
		},
	}

	a := newTestAdminAuthForTXSuccess(t, successStore)
	req := buildClaimSelectionRequest(t, "instance_admin")
	rr := httptest.NewRecorder()
	a.ClaimSelectionHandler(rr, req)

	// AC1 success path: handler must redirect 303 to dashboard.
	if rr.Code != http.StatusSeeOther {
		t.Errorf("AC1 FAIL: expected 303 SeeOther on success, got %d (body: %q)",
			rr.Code, rr.Body.String())
	}
	location := rr.Header().Get("Location")
	if location != "/admin/dashboard" {
		t.Errorf("AC1 FAIL: expected redirect to /admin/dashboard, got %q", location)
	}

	// AC1: oidc_issuer must be committed after successful TX.
	if successStore.committedIssuer == "" {
		t.Error("AC1 FAIL: oidc_issuer was not committed — saveBootstrapConfigTx did not persist")
	}

	// AC1: admin_group_claim must be committed after successful TX.
	if successStore.committedClaim == "" {
		t.Error("AC1 FAIL: admin_group_claim was not committed — saveAdminGroupClaimTx did not persist")
	}

	// AC4: clearDraftTx must be called inside the TX on the success path.
	if !successStore.clearDraftCalled {
		t.Error("AC4 FAIL: ClearDraft was not called on the success path — draft rows not cleared")
	}
}

// ---------------------------------------------------------------------------
// AC4 (negative): ClearDraft failure on success path must abort TX.
//
// Given:  bootstrap_active=true (completeBootstrapTx would succeed)
//         ClearDraft (clearDraftTx) returns an error
// When:   POST /admin/bootstrap/select-claim
// Then:   (a) handler returns 5xx (not 303) — TX was aborted
//         (b) no committed writes persist (oidc_issuer remains empty)
// ---------------------------------------------------------------------------
func TestClaimSelection_ClearDraftFailureAbortsTransaction(t *testing.T) {
	successStore := &txAwareConfigStoreSuccess{
		txAwareConfigStore: txAwareConfigStore{
			completeBootstrapErr:      nil,                               // CompleteBootstrap would succeed
			clearDraftErr:             errors.New("simulated ClearDraft DB failure"),
			bootstrapAlreadyCompleted: false,
		},
	}

	a := newTestAdminAuthForTXSuccess(t, successStore)
	req := buildClaimSelectionRequest(t, "instance_admin")
	rr := httptest.NewRecorder()
	a.ClaimSelectionHandler(rr, req)

	// AC4: ClearDraft failure must NOT result in a success redirect.
	if rr.Code == http.StatusSeeOther {
		t.Errorf("AC4 FAIL: handler returned 303 SeeOther despite ClearDraft failure — "+
			"TX should have been aborted. got: %d", rr.Code)
	}

	// AC4: no committed writes must persist when the TX was aborted due to ClearDraft failure.
	if successStore.committedIssuer != "" {
		t.Errorf("AC4 FAIL: oidc_issuer %q persisted in server_config despite ClearDraft TX abort — "+
			"TX must be rolled back when clearDraftTx fails", successStore.committedIssuer)
	}
}

// Ensure time import is used (referenced indirectly via saveBootstrapConfigTx in production;
// kept here to suppress unused-import errors if the store stubs don't need it directly).
var _ = time.Now
