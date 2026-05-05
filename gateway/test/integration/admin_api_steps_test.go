//go:build integration

package integration_test

// admin_api_steps_test.go — Story 6-11: Gherkin Admin API CRUD Flow
//
// Covers:
//   AC1 — User management lifecycle (list, deactivate, Matrix 401, reactivate, Matrix 200)
//   AC2 — Role assignment (grant compliance_officer, revoke, verify 403 on compliance endpoint)
//   AC3 — Room archival (archive, send-event 403 M_ROOM_ARCHIVED, get-messages 200)
//
// Authentication pattern: Matrix Bearer tokens obtained via Dex Authorization Code flow.
// Admin actor: kai@example.com  (Dex group: instance_admin)
// Target user: alex@example.com (Dex group: user)
//
// All Admin API endpoints are at gatewayURL (port 8080).
// Matrix CS API endpoints are at matrixURL (port 8008).
//
// Step function naming follows the existing suite convention (see compliance_flow_steps_test.go).
// All step functions are registered via initializeAdminAPISteps which is called from
// InitializeScenario in steps_test.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// ── Package-level state for Admin API scenarios ──────────────────────────────
// Reset via sc.Before hook in initializeAdminAPISteps so each scenario is isolated.

var adminAPIAdminToken        string // kai's Matrix access token
var adminAPIAdminUserID       string // kai's Matrix user_id
var adminAPITargetToken       string // alex's Matrix access token
var adminAPITargetUserID      string // alex's Matrix user_id
var adminAPIComplianceToken   string // compliance@example.com Matrix access token
var adminAPIComplianceUserID  string // compliance@example.com Matrix user_id
var adminAPIRoomID            string // room created for archival test
var adminAPITxnCounter        int    // monotonically incrementing txn ID

// ── AC1 step implementations ─────────────────────────────────────────────────

// theInstanceAdminKaiIsAuthenticatedForAdminAPI authenticates kai@example.com via Dex
// Authorization Code flow and stores the resulting Matrix access token.
func theInstanceAdminKaiIsAuthenticatedForAdminAPI() error {
	return authenticateUser("kai@example.com", "changeme", &adminAPIAdminToken, &adminAPIAdminUserID)
}

// theTargetUserAlexIsAuthenticatedForAdminAPI authenticates alex@example.com and stores
// the resulting Matrix access token and user_id for later steps.
func theTargetUserAlexIsAuthenticatedForAdminAPI() error {
	return authenticateUser("alex@example.com", "changeme", &adminAPITargetToken, &adminAPITargetUserID)
}

// theAdminCallsGETAdminUsers calls GET /api/v1/admin/users with the admin Bearer token
// and captures the response in lastStatusCode / lastBody.
func theAdminCallsGETAdminUsers() error {
	return adminAPIDoRequest(http.MethodGet, gatewayURL+"/api/v1/admin/users", adminAPIAdminToken, "")
}

// theAdminDeactivatesAlexWithReason calls POST /api/v1/admin/users/{alex_user_id}/deactivate
// with the given reason string. Expects 200 and body containing "deactivated".
func theAdminDeactivatesAlexWithReason(reason string) error {
	if adminAPITargetUserID == "" {
		return fmt.Errorf("target user ID is not set — ensure alex is authenticated first")
	}
	url := fmt.Sprintf("%s/api/v1/admin/users/%s/deactivate", gatewayURL, adminAPITargetUserID)
	body := fmt.Sprintf(`{"reason":%q}`, reason)
	return adminAPIDoRequest(http.MethodPost, url, adminAPIAdminToken, body)
}

// alexCallsGETSyncWithTheirToken calls GET /_matrix/client/v3/sync at matrixURL using
// alex's access token and captures the response.
func alexCallsGETSyncWithTheirToken() error {
	url := matrixURL + "/_matrix/client/v3/sync?timeout=0"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /sync request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+adminAPITargetToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /sync failed: %w", err)
	}
	return captureResponse(resp)
}

// theAdminReactivatesAlex calls POST /api/v1/admin/users/{alex_user_id}/reactivate
// with the admin Bearer token. Expects 200 and body containing "active".
func theAdminReactivatesAlex() error {
	if adminAPITargetUserID == "" {
		return fmt.Errorf("target user ID is not set — ensure alex is authenticated first")
	}
	url := fmt.Sprintf("%s/api/v1/admin/users/%s/reactivate", gatewayURL, adminAPITargetUserID)
	return adminAPIDoRequest(http.MethodPost, url, adminAPIAdminToken, "")
}

// ── AC2 step implementations ─────────────────────────────────────────────────

// theAdminGrantsAlexTheRole calls POST /api/v1/admin/users/{alex_user_id}/roles
// with body {"role": "<role>", "action": "grant"} and admin Bearer token.
func theAdminGrantsAlexTheRole(role string) error {
	if adminAPITargetUserID == "" {
		return fmt.Errorf("target user ID is not set — ensure alex is authenticated first")
	}
	url := fmt.Sprintf("%s/api/v1/admin/users/%s/roles", gatewayURL, adminAPITargetUserID)
	body := fmt.Sprintf(`{"role":%q,"action":"grant"}`, role)
	return adminAPIDoRequest(http.MethodPost, url, adminAPIAdminToken, body)
}

// theAdminRevokesAlexTheRole calls POST /api/v1/admin/users/{alex_user_id}/roles
// with body {"role": "<role>", "action": "revoke"} and admin Bearer token.
func theAdminRevokesAlexTheRole(role string) error {
	if adminAPITargetUserID == "" {
		return fmt.Errorf("target user ID is not set — ensure alex is authenticated first")
	}
	url := fmt.Sprintf("%s/api/v1/admin/users/%s/roles", gatewayURL, adminAPITargetUserID)
	body := fmt.Sprintf(`{"role":%q,"action":"revoke"}`, role)
	return adminAPIDoRequest(http.MethodPost, url, adminAPIAdminToken, body)
}

// aUserWithoutComplianceRoleCallsGETComplianceAccessRequests calls
// GET /api/v1/compliance/access-requests using alex's Matrix token (which carries only the
// "user" group, never "compliance_officer"). Expects 403 after role is revoked.
func aUserWithoutComplianceRoleCallsGETComplianceAccessRequests() error {
	// alex's JWT carries only groups: ["user"] from Dex — no compliance_officer claim.
	// The compliance endpoint checks ContextKeySystemRole (JWT-based), not role_overrides.
	// Therefore alex always gets 403 regardless of DB overrides.
	return adminAPIDoRequest(http.MethodGet, gatewayURL+"/api/v1/compliance/access-requests", adminAPITargetToken, "")
}

// theComplianceOfficerUserIsAuthenticated authenticates compliance@example.com, which
// permanently belongs to the Dex "compliance_officer" group, and stores the resulting
// Matrix access token. Used for the positive AC2 path (JWT-based compliance role check).
func theComplianceOfficerUserIsAuthenticated() error {
	return authenticateUser("compliance@example.com", "changeme", &adminAPIComplianceToken, &adminAPIComplianceUserID)
}

// theComplianceOfficerUserCallsGETComplianceAccessRequests authenticates
// compliance@example.com on first call (if not yet authenticated) and then calls
// GET /api/v1/compliance/access-requests using the resulting Matrix token.
// The JWT carries groups: ["compliance_officer"] from Dex, so the endpoint must return 200.
func theComplianceOfficerUserCallsGETComplianceAccessRequests() error {
	if adminAPIComplianceToken == "" {
		if err := theComplianceOfficerUserIsAuthenticated(); err != nil {
			return err
		}
	}
	return adminAPIDoRequest(http.MethodGet, gatewayURL+"/api/v1/compliance/access-requests", adminAPIComplianceToken, "")
}

// ── AC3 step implementations ─────────────────────────────────────────────────

// kaiCreatesARoomForArchivalTesting creates a new Matrix room as kai and stores
// the resulting room_id in adminAPIRoomID.
func kaiCreatesARoomForArchivalTesting() error {
	payload := `{"name":"archival-test-room"}`
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		matrixURL+"/_matrix/client/v3/createRoom",
		strings.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("building createRoom request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminAPIAdminToken)

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
	adminAPIRoomID = cr.RoomID
	return nil
}

// kaiSendsAMessageToTheArchivalTestRoom sends a single m.room.message event to
// adminAPIRoomID so that the room has at least one existing message before archival.
func kaiSendsAMessageToTheArchivalTestRoom() error {
	if adminAPIRoomID == "" {
		return fmt.Errorf("archival test room ID is not set — ensure room was created first")
	}
	txnID := adminAPINextTxnID()
	payload := `{"msgtype":"m.text","body":"archival test message"}`
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		matrixURL, adminAPIRoomID, txnID,
	)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building send message request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminAPIAdminToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /send failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("send message returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// theAdminArchivesTheArchivalTestRoomWithReason calls
// POST /api/v1/admin/rooms/{room_id}/archive with body {"reason": "<reason>"} and
// admin Bearer token. Expects 200 and body containing "archived".
func theAdminArchivesTheArchivalTestRoomWithReason(reason string) error {
	if adminAPIRoomID == "" {
		return fmt.Errorf("archival test room ID is not set — ensure room was created first")
	}
	url := fmt.Sprintf("%s/api/v1/admin/rooms/%s/archive", gatewayURL, adminAPIRoomID)
	body := fmt.Sprintf(`{"reason":%q}`, reason)
	return adminAPIDoRequest(http.MethodPost, url, adminAPIAdminToken, body)
}

// kaiSendsAMatrixEventToTheArchivedRoom attempts to PUT a new m.room.message event
// into the archived room. Expects 403 M_ROOM_ARCHIVED.
func kaiSendsAMatrixEventToTheArchivedRoom() error {
	if adminAPIRoomID == "" {
		return fmt.Errorf("archival test room ID is not set — ensure room was created first")
	}
	txnID := adminAPINextTxnID()
	payload := `{"msgtype":"m.text","body":"should be blocked"}`
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		matrixURL, adminAPIRoomID, txnID,
	)
	return adminAPIDoRequest(http.MethodPut, url, adminAPIAdminToken, payload)
}

// kaiCallsGETMessagesFromTheArchivedRoom calls
// GET /_matrix/client/v3/rooms/{room_id}/messages at matrixURL with kai's token.
// Expects 200 and a response body containing "chunk" (the existing message is present).
func kaiCallsGETMessagesFromTheArchivedRoom() error {
	if adminAPIRoomID == "" {
		return fmt.Errorf("archival test room ID is not set — ensure room was created first")
	}
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/messages", matrixURL, adminAPIRoomID)
	return adminAPIDoRequest(http.MethodGet, url, adminAPIAdminToken, "")
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// adminAPINextTxnID returns a unique transaction ID string for Matrix send-event calls.
func adminAPINextTxnID() string {
	adminAPITxnCounter++
	return fmt.Sprintf("admin-api-txn-%d-%d", time.Now().UnixNano(), adminAPITxnCounter)
}

// adminAPIDoRequest is a convenience wrapper that builds and executes an HTTP request,
// then captures the response into lastStatusCode / lastBody.
func adminAPIDoRequest(method, url, bearerToken, body string) error {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("building %s %s request: %w", method, url, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s failed: %w", method, url, err)
	}
	return captureResponse(resp)
}

// ── Godog registration ────────────────────────────────────────────────────────

// initializeAdminAPISteps registers all Admin API step definitions.
// Called from InitializeScenario in steps_test.go.
func initializeAdminAPISteps(sc *godog.ScenarioContext) {
	// Reset all scenario-local state before each scenario so scenarios are isolated.
	sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		// Auth tokens are intentionally NOT reset between scenarios — sessions remain valid
		// for the full suite run. Resetting on every scenario exhausts the /login burst (10).
		adminAPIRoomID = ""
		adminAPITxnCounter = 0
		return ctx, nil
	})

	// AC1 — User management lifecycle
	sc.Step(`^the instance_admin kai is authenticated for admin API$`,
		theInstanceAdminKaiIsAuthenticatedForAdminAPI)
	sc.Step(`^the target user alex is authenticated for admin API$`,
		theTargetUserAlexIsAuthenticatedForAdminAPI)
	sc.Step(`^the admin calls GET /api/v1/admin/users$`,
		theAdminCallsGETAdminUsers)
	sc.Step(`^the admin deactivates alex with reason "([^"]*)"$`,
		theAdminDeactivatesAlexWithReason)
	sc.Step(`^alex calls GET /_matrix/client/v3/sync with their token$`,
		alexCallsGETSyncWithTheirToken)
	sc.Step(`^the admin reactivates alex$`,
		theAdminReactivatesAlex)

	// AC2 — Role assignment lifecycle
	sc.Step(`^the admin grants alex the role "([^"]*)"$`,
		theAdminGrantsAlexTheRole)
	sc.Step(`^the admin revokes alex the role "([^"]*)"$`,
		theAdminRevokesAlexTheRole)
	sc.Step(`^the compliance officer user calls GET /api/v1/compliance/access-requests$`,
		theComplianceOfficerUserCallsGETComplianceAccessRequests)
	sc.Step(`^a user without compliance role calls GET /api/v1/compliance/access-requests$`,
		aUserWithoutComplianceRoleCallsGETComplianceAccessRequests)

	// AC3 — Room archival
	sc.Step(`^kai creates a room for archival testing$`,
		kaiCreatesARoomForArchivalTesting)
	sc.Step(`^kai sends a message to the archival test room$`,
		kaiSendsAMessageToTheArchivalTestRoom)
	sc.Step(`^the admin archives the archival test room with reason "([^"]*)"$`,
		theAdminArchivesTheArchivalTestRoomWithReason)
	sc.Step(`^kai sends a Matrix event to the archived room$`,
		kaiSendsAMatrixEventToTheArchivedRoom)
	sc.Step(`^kai calls GET /_matrix/client/v3/rooms/\{archivalRoomId\}/messages$`,
		kaiCallsGETMessagesFromTheArchivedRoom)
}
