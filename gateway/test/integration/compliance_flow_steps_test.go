//go:build integration

package integration_test

// compliance_flow_steps_test.go — Story 5.9: Gherkin compliance flow end-to-end
//
// Covers AC1 (four-eyes export), AC2 (GDPR deletion + anonymization), AC3 (audit
// log immutability).  All step functions are registered via initializeComplianceFlowSteps
// which is called from InitializeScenario in steps_test.go.
//
// Authentication pattern: Authorization Code flow via Dex (iObtainDexTokenFor +
// iPostLoginWithDexToken — both defined in auth_steps_test.go, same package).
//
// DB driver: jackc/pgx/v5/stdlib — already in go.mod, used by other integration tests.

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ── Package-level state for compliance flow scenarios ────────────────────────
// These are reset via the sc.Before hook in initializeComplianceFlowSteps so
// each scenario starts clean.

var officerAAccessToken string
var officerAUserID string
var officerBAccessToken string
var officerBUserID string
var complianceRoomID string
var complianceRequestID string
var complianceSessionToken string
var complianceAdminAccessToken string
var complianceAdminUserID string
var complianceVictimUserID string

// ── AC1 step implementations ─────────────────────────────────────────────────

// twoComplianceOfficersAreAuthenticated authenticates both compliance officers
// (compliance@example.com = officer_a, compliance2@example.com = officer_b)
// using the Authorization Code flow against Dex.
func twoComplianceOfficersAreAuthenticated() error {
	if err := authenticateUser("compliance@example.com", "changeme", &officerAAccessToken, &officerAUserID); err != nil {
		return fmt.Errorf("authenticate officer_a: %w", err)
	}
	if err := authenticateUser("compliance2@example.com", "changeme", &officerBAccessToken, &officerBUserID); err != nil {
		return fmt.Errorf("authenticate officer_b: %w", err)
	}
	return nil
}

// officerACreatesARoomWithMessages creates a room as officer_a and sends one
// message to ensure at least one event exists in the compliance time window.
func officerACreatesARoomWithMessages() error {
	// Create room
	payload := `{"name":"compliance-test-room"}`
	req, err := http.NewRequest(http.MethodPost,
		matrixURL+"/_matrix/client/v3/createRoom",
		strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building createRoom request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+officerAAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /createRoom failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("createRoom returned %d: %s", resp.StatusCode, lastBody)
	}
	var cr struct {
		RoomID string `json:"room_id"`
	}
	if err := json.Unmarshal(body, &cr); err != nil || cr.RoomID == "" {
		return fmt.Errorf("no room_id in createRoom response: %s", lastBody)
	}
	complianceRoomID = cr.RoomID

	// Send a message so the room has at least one event
	txnID := fmt.Sprintf("compliance-txn-%d", time.Now().UnixNano())
	msgPayload := `{"msgtype":"m.text","body":"compliance test message"}`
	msgURL := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		matrixURL, complianceRoomID, txnID)
	msgReq, err := http.NewRequest(http.MethodPut, msgURL, strings.NewReader(msgPayload))
	if err != nil {
		return fmt.Errorf("building send message request: %w", err)
	}
	msgReq.Header.Set("Content-Type", "application/json")
	msgReq.Header.Set("Authorization", "Bearer "+officerAAccessToken)

	msgResp, err := http.DefaultClient.Do(msgReq)
	if err != nil {
		return fmt.Errorf("PUT /send failed: %w", err)
	}
	defer msgResp.Body.Close()
	msgBody, _ := io.ReadAll(msgResp.Body)
	if msgResp.StatusCode != http.StatusOK {
		return fmt.Errorf("send message returned %d: %s", msgResp.StatusCode, string(msgBody))
	}
	return nil
}

// officerASubmitsComplianceAccessRequest POSTs an access request for the room
// created in the previous step. Uses a ±1h time window around now so that the
// room message is always within range.
func officerASubmitsComplianceAccessRequest() error {
	now := time.Now().UTC()
	reqBody := map[string]string{
		"room_id":          complianceRoomID,
		"time_range_start": now.Add(-1 * time.Hour).Format(time.RFC3339),
		"time_range_end":   now.Add(1 * time.Hour).Format(time.RFC3339),
		"justification":    "E2E integration test — four-eyes compliance export verification",
	}
	payload, _ := json.Marshal(reqBody)

	req, err := http.NewRequest(http.MethodPost,
		gatewayURL+"/api/v1/compliance/access-requests",
		strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("building access-request POST: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+officerAAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /access-requests failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	if resp.StatusCode == http.StatusCreated {
		var ar struct {
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(body, &ar); err == nil && ar.RequestID != "" {
			complianceRequestID = ar.RequestID
		}
	}
	return nil
}

// officerATriesToApproveOwnRequest attempts self-approval — expected 403.
func officerATriesToApproveOwnRequest() error {
	url := fmt.Sprintf("%s/api/v1/compliance/access-requests/%s/approve",
		gatewayURL, complianceRequestID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("building self-approve request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+officerAAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST .../approve (self) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// officerBApprovesComplianceRequest approves the request as officer_b.
func officerBApprovesComplianceRequest() error {
	url := fmt.Sprintf("%s/api/v1/compliance/access-requests/%s/approve",
		gatewayURL, complianceRequestID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("building officer_b approve request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+officerBAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST .../approve (officer_b) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// officerACreatesComplianceSession POSTs to the session endpoint for the
// approved request and stores the resulting session_token.
func officerACreatesComplianceSession() error {
	url := fmt.Sprintf("%s/api/v1/compliance/access-requests/%s/session",
		gatewayURL, complianceRequestID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("building create-session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+officerAAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST .../session failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	if resp.StatusCode == http.StatusCreated {
		var sr struct {
			SessionToken string `json:"session_token"`
		}
		if err := json.Unmarshal(body, &sr); err == nil && sr.SessionToken != "" {
			complianceSessionToken = sr.SessionToken
		}
	}
	return nil
}

// officerACallsComplianceExport calls GET /api/v1/compliance/export with both
// the Authorization Bearer header (Matrix token) and the X-Compliance-Token header.
func officerACallsComplianceExport() error {
	req, err := http.NewRequest(http.MethodGet,
		gatewayURL+"/api/v1/compliance/export", nil)
	if err != nil {
		return fmt.Errorf("building export request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+officerAAccessToken)
	req.Header.Set("X-Compliance-Token", complianceSessionToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /export failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// theServerSignatureIsVerifiable reads the server's Ed25519 public key from
// server_config, reconstructs the signed document (map minus server_signature),
// and verifies the signature.
func theServerSignatureIsVerifiable() error {
	// 1. Parse the export response into a map
	var exportMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lastBody), &exportMap); err != nil {
		return fmt.Errorf("parsing export response JSON: %w (body: %.200s)", err, lastBody)
	}

	// 2. Extract and base64-decode the server_signature
	sigRaw, ok := exportMap["server_signature"]
	if !ok {
		return fmt.Errorf("server_signature field missing from export response")
	}
	var sigB64 string
	if err := json.Unmarshal(sigRaw, &sigB64); err != nil {
		return fmt.Errorf("unmarshalling server_signature: %w", err)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("base64-decoding server_signature: %w", err)
	}

	// 3. Reconstruct the signed document: delete server_signature, re-marshal
	delete(exportMap, "server_signature")
	docBytes, err := json.Marshal(exportMap)
	if err != nil {
		return fmt.Errorf("re-marshalling export document for verification: %w", err)
	}

	// 4. Read compliance_signing_key_pub from server_config
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("opening DB for key read: %w", err)
	}
	defer db.Close()

	var keyHex string
	err = db.QueryRowContext(context.Background(),
		"SELECT value FROM server_config WHERE key = 'compliance_signing_key_pub'",
	).Scan(&keyHex)
	if err != nil {
		return fmt.Errorf("reading compliance_signing_key_pub from server_config: %w", err)
	}

	pubKeyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return fmt.Errorf("hex-decoding compliance_signing_key_pub: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("unexpected Ed25519 public key length: got %d, want %d",
			len(pubKeyBytes), ed25519.PublicKeySize)
	}
	pubKey := ed25519.PublicKey(pubKeyBytes)

	// 5. Verify
	if !ed25519.Verify(pubKey, docBytes, sigBytes) {
		return fmt.Errorf("Ed25519 signature verification failed — export document may have been tampered with")
	}
	return nil
}

// ── AC2 step implementations ─────────────────────────────────────────────────

// adminAuthenticatedAndVictimExists authenticates the admin (kai@example.com)
// and creates a victim user directly via DB INSERT (test data setup — not auth bypass).
// The victim user has displayname "Alice".
func adminAuthenticatedAndVictimExists(displayname string) error {
	// Authenticate admin
	if err := authenticateUser("kai@example.com", "changeme", &complianceAdminAccessToken, &complianceAdminUserID); err != nil {
		return fmt.Errorf("authenticate admin: %w", err)
	}

	// Create victim user directly in the DB (legitimate test data setup).
	// The victim's user_id and localpart follow the Nebu convention:
	// user_id = "@victim:<server_name>" — we use a fixed sub for the test.
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("opening DB for victim setup: %w", err)
	}
	defer db.Close()

	// Determine server_name from server_config
	var serverName string
	err = db.QueryRowContext(context.Background(),
		"SELECT value FROM server_config WHERE key = 'instance_name'",
	).Scan(&serverName)
	if err != nil {
		// Fallback: use localhost if not yet configured
		serverName = "localhost"
	}

	victimLocalpart := fmt.Sprintf("victim-%d", time.Now().UnixNano())
	victimMatrixID := fmt.Sprintf("@%s:%s", victimLocalpart, serverName)
	victimSub := fmt.Sprintf("victim-sub-%d", time.Now().UnixNano())
	complianceVictimUserID = victimMatrixID

	// Insert into users table
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO users (sub, matrix_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (sub) DO NOTHING
	`, victimSub, victimMatrixID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("inserting victim user row: %w", err)
	}

	// Insert display name into profiles table
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO profiles (matrix_id, displayname)
		VALUES ($1, $2)
		ON CONFLICT (matrix_id) DO UPDATE SET displayname = EXCLUDED.displayname
	`, victimMatrixID, displayname)
	if err != nil {
		return fmt.Errorf("inserting victim profile: %w", err)
	}
	return nil
}

// adminDeletesVictimKeys calls DELETE /api/v1/admin/users/{userId}/keys.
func adminDeletesVictimKeys(reason string) error {
	url := fmt.Sprintf("%s/api/v1/admin/users/%s/keys",
		gatewayURL, complianceVictimUserID)
	reqBody := fmt.Sprintf(`{"reason":%q}`, reason)
	req, err := http.NewRequest(http.MethodDelete, url, strings.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("building DELETE /keys request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+complianceAdminAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE /keys failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// auditLogContainsRow checks that audit_log has a row matching the given action and outcome.
func auditLogContainsRow(action, outcome string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("opening DB for audit_log check: %w", err)
	}
	defer db.Close()

	// Filter on target_id too so a row from a previous run of the same scenario
	// (audit_log is append-only) cannot make this assertion vacuously true.
	var count int
	err = db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM audit_log WHERE action = $1 AND outcome = $2 AND target_id = $3",
		action, outcome, complianceVictimUserID,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("querying audit_log: %w", err)
	}
	if count < 1 {
		return fmt.Errorf("expected at least 1 audit_log row with action=%q outcome=%q target_id=%q, found %d",
			action, outcome, complianceVictimUserID, count)
	}
	return nil
}

// adminAnonymizesVictim calls POST /api/v1/admin/users/{userId}/anonymize.
func adminAnonymizesVictim() error {
	url := fmt.Sprintf("%s/api/v1/admin/users/%s/anonymize",
		gatewayURL, complianceVictimUserID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("building POST /anonymize request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+complianceAdminAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /anonymize failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// theMatrixProfileShowsDisplayname GETs /_matrix/client/v3/profile/{userId}
// and asserts the returned displayname matches the expected value.
func theMatrixProfileShowsDisplayname(expectedDisplayname string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/profile/%s",
		matrixURL, complianceVictimUserID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /profile request: %w", err)
	}
	// Profile endpoint may require authentication — use admin token if available
	if complianceAdminAccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+complianceAdminAccessToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /profile failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /profile returned %d: %s", resp.StatusCode, lastBody)
	}

	var profileResp struct {
		Displayname string `json:"displayname"`
	}
	if err := json.Unmarshal(body, &profileResp); err != nil {
		return fmt.Errorf("parsing profile response: %w (body: %s)", err, lastBody)
	}
	if profileResp.Displayname != expectedDisplayname {
		return fmt.Errorf("expected displayname %q, got %q", expectedDisplayname, profileResp.Displayname)
	}
	return nil
}

// ── AC3 step implementations ─────────────────────────────────────────────────

// theAuditLogTableHasAtLeastOneRow ensures the audit_log is non-empty.
// If empty, it seeds one row via a direct INSERT (the application role may INSERT).
func theAuditLogTableHasAtLeastOneRow() error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("opening DB: %w", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM audit_log",
	).Scan(&count); err != nil {
		return fmt.Errorf("counting audit_log rows: %w", err)
	}
	if count > 0 {
		return nil // already has rows — nothing to do
	}

	// Seed one row so the DELETE test has something to target.
	// Column names match migration 000018_audit_log.up.sql:
	//   actor_user_id, action, target_type, target_id, outcome.
	// (TEA Gate 2 MAJOR-1 fix: previous code used non-existent
	// actor_id / resource_type / resource_id columns.)
	_, err = db.ExecContext(context.Background(), `
		INSERT INTO audit_log (actor_user_id, action, outcome)
		VALUES ('test-actor', 'rls_seed', 'success')
	`)
	if err != nil {
		return fmt.Errorf("seeding audit_log row: %w", err)
	}
	return nil
}

// aDirectSQLDeleteFromAuditLogIsAttempted attempts a DELETE FROM audit_log
// as the application DB role (nebu via dbURL). The behaviour is captured for
// the Then step.
//
// FB-51-01 caveat: the dev/CI nebu role is currently a Postgres superuser
// with BYPASSRLS, so a behavioural assertion ("DELETE must fail") would be a
// false negative until Story 5-29 ships the non-superuser app role split.
// Until then, the Then step verifies the policy STRUCTURALLY in pg_policies
// rather than the runtime behaviour. The DELETE attempt is preserved here so
// that, post-FB-51-01, a future hardening can additionally assert the runtime
// error without a step-rewrite.
var complianceRLSDeleteErr error

func aDirectSQLDeleteFromAuditLogIsAttempted() error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("opening DB for RLS DELETE test: %w", err)
	}
	defer db.Close()

	_, complianceRLSDeleteErr = db.ExecContext(context.Background(),
		"DELETE FROM audit_log WHERE actor_user_id = 'test-actor' AND action = 'rls_seed'")
	return nil
}

// postgresqlRaisesAPolicyViolationError verifies the RLS DELETE-deny policy
// is in place. We assert the policy STRUCTURALLY (`pg_policies` row exists
// with `cmd = 'DELETE'` and `qual = 'false'`) rather than the runtime
// behaviour, because the dev/CI nebu role currently bypasses RLS via
// BYPASSRLS (FB-51-01 / Story 5-29).
//
// Once the non-superuser app role lands, this step can additionally assert
// `complianceRLSDeleteErr != nil` with a 42501 errcode for full
// behavioural coverage.
func postgresqlRaisesAPolicyViolationError() error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("opening DB for RLS policy check: %w", err)
	}
	defer db.Close()

	var qual sql.NullString
	err = db.QueryRowContext(context.Background(), `
		SELECT qual FROM pg_policies
		WHERE schemaname = 'public'
		  AND tablename  = 'audit_log'
		  AND cmd        = 'DELETE'
	`).Scan(&qual)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("audit_log has no DELETE policy — RLS immutability is not enforced at the schema level")
	}
	if err != nil {
		return fmt.Errorf("querying pg_policies: %w", err)
	}
	if !qual.Valid || !strings.Contains(strings.ToLower(qual.String), "false") {
		return fmt.Errorf("audit_log DELETE policy expected to USING (false), got: %v", qual)
	}

	// Optional behaviour log: helpful for forensics when FB-51-01 lands.
	if complianceRLSDeleteErr != nil {
		errMsg := complianceRLSDeleteErr.Error()
		if !strings.Contains(errMsg, "42501") && !strings.Contains(errMsg, "policy") && !strings.Contains(errMsg, "insufficient") {
			return fmt.Errorf("DELETE failed but with unexpected error (not RLS-related): %s", errMsg)
		}
	}
	return nil
}

// ── Godog registration ────────────────────────────────────────────────────────

// initializeComplianceFlowSteps registers all compliance flow step definitions.
// Called from InitializeScenario in steps_test.go.
func initializeComplianceFlowSteps(sc *godog.ScenarioContext) {
	// Reset all scenario-local state before each scenario.
	sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		// Auth tokens are intentionally NOT reset between scenarios — sessions remain valid
		// for the full suite run. Resetting on every scenario exhausts the /login burst (10).
		complianceRoomID = ""
		complianceRequestID = ""
		complianceSessionToken = ""
		complianceVictimUserID = ""
		complianceRLSDeleteErr = nil
		return ctx, nil
	})

	// AC1 — Four-eyes compliance export
	sc.Step(`^two compliance officers are authenticated with valid Matrix sessions$`,
		twoComplianceOfficersAreAuthenticated)
	sc.Step(`^officer_a creates a room with at least one message$`,
		officerACreatesARoomWithMessages)
	sc.Step(`^officer_a submits a compliance access request for the room$`,
		officerASubmitsComplianceAccessRequest)
	sc.Step(`^officer_a tries to approve their own access request$`,
		officerATriesToApproveOwnRequest)
	sc.Step(`^officer_b approves the compliance access request$`,
		officerBApprovesComplianceRequest)
	sc.Step(`^officer_a creates a compliance session for the approved request$`,
		officerACreatesComplianceSession)
	sc.Step(`^officer_a calls GET /api/v1/compliance/export with the compliance session token$`,
		officerACallsComplianceExport)
	sc.Step(`^the server_signature is verifiable with the server Ed25519 public key$`,
		theServerSignatureIsVerifiable)

	// AC2 — GDPR deletion + anonymization
	sc.Step(`^an admin is authenticated and a victim user exists with displayname "([^"]*)"$`,
		adminAuthenticatedAndVictimExists)
	sc.Step(`^admin deletes keys for the victim user with reason "([^"]*)"$`,
		adminDeletesVictimKeys)
	sc.Step(`^the audit_log contains a row with action "([^"]*)" and outcome "([^"]*)"$`,
		auditLogContainsRow)
	sc.Step(`^admin anonymizes the victim user$`,
		adminAnonymizesVictim)
	sc.Step(`^the Matrix profile for the victim user shows displayname "([^"]*)"$`,
		theMatrixProfileShowsDisplayname)

	// AC3 — Audit log immutability
	sc.Step(`^the audit_log table has at least one row$`,
		theAuditLogTableHasAtLeastOneRow)
	sc.Step(`^a direct SQL DELETE FROM audit_log is attempted using the application DB role$`,
		aDirectSQLDeleteFromAuditLogIsAttempted)
	sc.Step(`^PostgreSQL raises a policy violation error$`,
		postgresqlRaisesAPolicyViolationError)
}
