package admin

// RED PHASE — Story 11-10 AC2: Bootstrap Wizard Claim Mapping — Atomic Transaction
//
// Verifies that the three new OIDC claim-mapping keys (oidc_user_id_claim,
// oidc_displayname_claim, oidc_email_claim) are persisted atomically in the same
// sql.Tx as bootstrap_completed and admin_group_claim during Bootstrap Step 3.
//
// FAILS until:
//   - BootstrapClaimMappingStep is added to auth.go (or as a new handler)
//   - ClaimSelectionHandler (or new Step 3 handler) accepts and persists
//     oidc_user_id_claim, oidc_displayname_claim, oidc_email_claim fields
//     inside the existing runInTx call that writes bootstrap_completed
//
// Pattern: mirrors claim_selection_tx_test.go (Story 5.11).

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// errRollback is returned by fakeCompleteBootstrap to force a rollback in AC2 tests.
var errRollbackClaimMapping = errors.New("simulated rollback for claim mapping tx test")

// TestBootstrapStep3_ClaimMappingFields_PersistAtomically verifies that
// oidc_user_id_claim, oidc_displayname_claim, oidc_email_claim are all written
// inside the same runInTx call that commits bootstrap_completed.
//
// AC2 — "The claim-mapping fields are persisted atomically in the bootstrap transaction."
//
// This test FAILS until BootstrapClaimMappingStepHandler (or the extended
// ClaimSelectionHandler) writes all three keys within a single runInTx call.
func TestBootstrapStep3_ClaimMappingFields_PersistAtomically(t *testing.T) {
	store := &txAwareConfigStore{}
	auth := newTestAdminAuthForTX(t, store)

	form := url.Values{}
	form.Set("admin_group_claim", "groups")
	form.Set("oidc_user_id_claim", "sub")
	form.Set("oidc_displayname_claim", "name")
	form.Set("oidc_email_claim", "email")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap/select-claim", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	auth.ClaimSelectionHandler(w, req)

	// Verify all four keys are written
	for _, key := range []string{"admin_group_claim", "oidc_user_id_claim", "oidc_displayname_claim", "oidc_email_claim"} {
		if _, ok := store.committed[key]; !ok {
			t.Errorf("expected %q to be committed to server_config, but it was not", key)
		}
	}
	// Verify bootstrap_completed is also committed
	if _, ok := store.committed["bootstrap_completed"]; !ok {
		t.Error("expected bootstrap_completed to be committed in the same transaction")
	}
}

// TestBootstrapStep3_ClaimMappingRollback_NoPersistence verifies that if the
// transaction is rolled back (e.g., bootstrap already completed), NONE of the
// three claim-mapping keys are persisted.
//
// AC2 — rollback must leave server_config unchanged.
//
// This test FAILS until BootstrapClaimMappingStepHandler writes claim keys inside
// the tx (not before it), so a rollback purges them.
func TestBootstrapStep3_ClaimMappingRollback_NoPersistence(t *testing.T) {
	store := &txAwareConfigStore{completeBootstrapErr: errRollbackClaimMapping}
	auth := newTestAdminAuthForTX(t, store)

	form := url.Values{}
	form.Set("admin_group_claim", "groups")
	form.Set("oidc_user_id_claim", "sub")
	form.Set("oidc_displayname_claim", "name")
	form.Set("oidc_email_claim", "email")
	body := strings.NewReader(form.Encode())

	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap/select-claim", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	auth.ClaimSelectionHandler(w, req)

	// After rollback, none of the claim keys should be in committed state
	for _, key := range []string{"oidc_user_id_claim", "oidc_displayname_claim", "oidc_email_claim"} {
		if _, ok := store.committed[key]; ok {
			t.Errorf("expected %q NOT to be committed after rollback, but it was", key)
		}
	}
}
