//go:build integration

package integration_test

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

// Room flow state — package-level, shared across all step files in integration_test.
// These are populated by kaiIsAuthenticated / alexIsAuthenticated and carried through
// the scenario steps. Godog runs scenarios sequentially so there is no concurrency risk.
var lastRoomID string
var lastEventID string
var kaiAccessToken string
var alexAccessToken string
var marieAccessToken string
var kaiUserID string
var alexUserID string
var marieUserID string
var lastTxnID string
var lastSecondEventID string
var lastMsgBody string
var marieCapturedSyncToken string
var marieIncrementalSyncBody string
var kaiCapturedSyncToken string
var kaiIncrementalSyncBody string
var kaiInitialSyncBody string // Story 9-24: top-level account_data in initial sync

// authenticateUser runs the Dex authorization code flow for the given credentials
// and then performs POST /login on the Matrix endpoint. The resulting access_token
// and user_id are stored via the provided pointer arguments.
//
// Implementation notes:
//   - iObtainDexTokenFor is defined in auth_steps_test.go (same package) — callable directly.
//   - iPostLoginWithDexToken is likewise in auth_steps_test.go — callable directly.
//   - iObtainDexTokenFor stores its result in lastDexIDToken (auth_steps_test.go).
//   - iPostLoginWithDexToken stores the access_token in lastAccessToken (auth_steps_test.go).
//   - We copy lastAccessToken → *accessToken before calling the next user's auth (which
//     would overwrite lastAccessToken).
func authenticateUser(username, password string, accessToken, userID *string) error {
	// Idempotent: skip re-login if a token is already cached for this variable.
	// The /login endpoint has burst=10; with 40+ scenarios each calling a Background
	// auth step, repeated logins exhaust the bucket and cause 429s.
	if *accessToken != "" {
		return nil
	}
	// Step 1: Dex authorization code flow → lastDexIDToken
	if err := iObtainDexTokenFor(username, password); err != nil {
		return fmt.Errorf("Dex auth for %s: %w", username, err)
	}
	// Step 2: Matrix /login → lastAccessToken + lastBody
	if err := iPostLoginWithDexToken(); err != nil {
		return fmt.Errorf("Matrix login for %s: %w", username, err)
	}
	if lastStatusCode != http.StatusOK {
		return fmt.Errorf("POST /login for %s: expected 200, got %d (body: %s)", username, lastStatusCode, lastBody)
	}
	// Step 3: Parse access_token and user_id from the login response body.
	var loginResp struct {
		AccessToken string `json:"access_token"`
		UserID      string `json:"user_id"`
	}
	if err := json.Unmarshal([]byte(lastBody), &loginResp); err != nil {
		return fmt.Errorf("parsing /login response for %s: %w", username, err)
	}
	if loginResp.AccessToken == "" {
		return fmt.Errorf("empty access_token in /login response for %s", username)
	}
	*accessToken = loginResp.AccessToken
	*userID = loginResp.UserID
	return nil
}

// kaiIsAuthenticated authenticates kai@example.com via Dex and Matrix login,
// storing the result in kaiAccessToken and kaiUserID.
// Idempotent: skips re-login if a token is already cached. This avoids exhausting
// the /login rate-limit bucket (burst=10) when many Godog scenarios share the
// same Background step across a full suite run.
func kaiIsAuthenticated() error {
	if kaiAccessToken != "" {
		return nil
	}
	return authenticateUser("kai@example.com", "changeme", &kaiAccessToken, &kaiUserID)
}

// alexIsAuthenticated authenticates alex@example.com via Dex and Matrix login,
// storing the result in alexAccessToken and alexUserID.
// Idempotent: skips re-login if a token is already cached.
func alexIsAuthenticated() error {
	if alexAccessToken != "" {
		return nil
	}
	return authenticateUser("alex@example.com", "changeme", &alexAccessToken, &alexUserID)
}

// kaiCreatesARoom calls POST /_matrix/client/v3/createRoom with the given name,
// stores the returned room_id in lastRoomID, and updates lastStatusCode/lastBody.
func kaiCreatesARoom(name string) error {
	payload := fmt.Sprintf(`{"name":%q}`, name)
	req, err := http.NewRequest(
		http.MethodPost,
		matrixURL+"/_matrix/client/v3/createRoom",
		strings.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("building createRoom request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

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
	lastRoomID = cr.RoomID
	return nil
}

// kaiInvitesAlex calls POST /_matrix/client/v3/rooms/{lastRoomID}/invite
// with alexUserID as the invited user.
func kaiInvitesAlex() error {
	payload := fmt.Sprintf(`{"user_id":%q}`, alexUserID)
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/invite", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building invite request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /invite failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invite returned %d: %s", resp.StatusCode, lastBody)
	}
	return nil
}

// alexJoinsTheRoom calls POST /_matrix/client/v3/join/{lastRoomID} as alex.
func alexJoinsTheRoom() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/join/%s", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("building join request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /join failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("join returned %d: %s", resp.StatusCode, lastBody)
	}
	return nil
}

// kaiSendsMessage calls PUT /_matrix/client/v3/rooms/{lastRoomID}/send/m.room.message/{txnId}
// with a new unique txnId. The txnId is saved in lastTxnID for the idempotency scenario.
// The returned event_id is stored in lastEventID.
func kaiSendsMessage(msgBody string) error {
	lastTxnID = fmt.Sprintf("txn-%d", time.Now().UnixNano())
	lastMsgBody = msgBody
	payload := fmt.Sprintf(`{"msgtype":"m.text","body":%q}`, msgBody)
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		matrixURL, lastRoomID, lastTxnID,
	)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building sendEvent request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /send failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	if resp.StatusCode == http.StatusOK {
		var sr struct {
			EventID string `json:"event_id"`
		}
		if err := json.Unmarshal(body, &sr); err == nil {
			lastEventID = sr.EventID
		}
	}
	return nil
}

// kaiSendsTheSameMessageAgain repeats the PUT /send call with the identical txnId stored
// in lastTxnID. The second response's event_id is stored in lastSecondEventID so that
// bothSendsReturnedTheSameEventID can compare the two.
func kaiSendsTheSameMessageAgain() error {
	// Reuse lastTxnID — the point of the idempotency test is that the same txnId
	// must return the same event_id regardless of how many times it is replayed.
	payload := fmt.Sprintf(`{"msgtype":"m.text","body":%q}`, lastMsgBody)
	url := fmt.Sprintf(
		"%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		matrixURL, lastRoomID, lastTxnID,
	)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building second sendEvent request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /send (second) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	if resp.StatusCode == http.StatusOK {
		var sr struct {
			EventID string `json:"event_id"`
		}
		if err := json.Unmarshal(body, &sr); err == nil {
			lastSecondEventID = sr.EventID
		}
	}
	return nil
}

// alexRetrievesMessagesFromTheRoom calls GET /_matrix/client/v3/rooms/{lastRoomID}/messages
// authenticated as alex and stores the result in lastStatusCode/lastBody.
func alexRetrievesMessagesFromTheRoom() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/messages", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building getMessages request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /messages (alex) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiRetrievesMessagesFromTheRoom calls GET /_matrix/client/v3/rooms/{lastRoomID}/messages
// authenticated as kai and stores the result in lastStatusCode/lastBody.
// Used in the idempotency scenario where alex has not joined the room.
func kaiRetrievesMessagesFromTheRoom() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/messages", matrixURL, lastRoomID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building getMessages request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /messages (kai) failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// bothSendsReturnedTheSameEventID asserts that lastEventID and lastSecondEventID are
// non-empty and identical — the txnId idempotency guarantee.
func bothSendsReturnedTheSameEventID() error {
	if lastEventID == "" {
		return fmt.Errorf("lastEventID is empty — first send did not return an event_id")
	}
	if lastSecondEventID == "" {
		return fmt.Errorf("lastSecondEventID is empty — second send did not return an event_id")
	}
	if lastEventID != lastSecondEventID {
		return fmt.Errorf(
			"txnId idempotency failed: first event_id=%q, second event_id=%q",
			lastEventID, lastSecondEventID,
		)
	}
	return nil
}

// theBodyContainsExactlyOnce parses the /messages response and counts m.room.message
// events whose content.body equals substr. Used in the idempotency scenario to confirm
// the message was stored only once despite the duplicate txnId.
func theBodyContainsExactlyOnce(substr string) error {
	// Parse the /messages response and count m.room.message events
	// where content.body == substr
	var messagesResp struct {
		Chunk []struct {
			Type    string `json:"type"`
			Content struct {
				Body string `json:"body"`
			} `json:"content"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal([]byte(lastBody), &messagesResp); err != nil {
		return fmt.Errorf("failed to parse messages response: %w", err)
	}
	count := 0
	for _, event := range messagesResp.Chunk {
		if event.Type == "m.room.message" && event.Content.Body == substr {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("expected exactly 1 message with body %q in chunk, got %d", substr, count)
	}
	return nil
}

// marieIsAuthenticated authenticates marie@example.com.
// Idempotent: skips re-login if a token is already cached.
func marieIsAuthenticated() error {
	if marieAccessToken != "" {
		return nil
	}
	return authenticateUser("marie@example.com", "changeme", &marieAccessToken, &marieUserID)
}

// alexCreatesARoom creates a room as alex (not kai).
func alexCreatesARoom(name string) error {
	payload := fmt.Sprintf(`{"name":%q}`, name)
	req, _ := http.NewRequest(http.MethodPost, matrixURL+"/_matrix/client/v3/createRoom", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /createRoom: %w", err)
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
		return fmt.Errorf("no room_id in response: %s", lastBody)
	}
	lastRoomID = cr.RoomID
	return nil
}

// alexInvitesMarie invites marie to the last created room as alex.
func alexInvitesMarie() error {
	payload := fmt.Sprintf(`{"user_id":%q}`, marieUserID)
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/invite", matrixURL, lastRoomID)
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /invite: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invite returned %d: %s", resp.StatusCode, lastBody)
	}
	return nil
}

// marieCapturesSyncToken performs GET /sync?timeout=0 as marie and stores next_batch.
// This token is used to detect changes that happen AFTER this point (the join).
func marieCapturesSyncTokenBeforeJoining() error {
	req, _ := http.NewRequest(http.MethodGet,
		matrixURL+"/_matrix/client/v3/sync?timeout=0", nil)
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /sync (pre-join): %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sync returned %d: %s", resp.StatusCode, string(body))
	}
	var syncResp struct {
		NextBatch string `json:"next_batch"`
	}
	if err := json.Unmarshal(body, &syncResp); err != nil || syncResp.NextBatch == "" {
		return fmt.Errorf("no next_batch in pre-join sync: %s", string(body))
	}
	marieCapturedSyncToken = syncResp.NextBatch
	return nil
}

// marieJoinsTheRoom calls POST /join/{lastRoomID} as marie.
func marieJoinsTheRoom() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/join/%s", matrixURL, lastRoomID)
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /join: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("join returned %d: %s", resp.StatusCode, lastBody)
	}
	return nil
}

// marieCallsIncrementalSync calls GET /sync?since=<captured_token>&timeout=0 as marie.
func marieCallsIncrementalSyncWithCapturedToken() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/sync?since=%s&timeout=0",
		matrixURL, marieCapturedSyncToken)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+marieAccessToken)
	// Retry up to 3 times with short delay — sync may need a moment to process the join.
	var body []byte
	var statusCode int
	for i := 0; i < 3; i++ {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("GET /sync (incremental): %w", err)
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		statusCode = resp.StatusCode
		if statusCode == http.StatusOK {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if statusCode != http.StatusOK {
		return fmt.Errorf("incremental sync returned %d: %s", statusCode, string(body))
	}
	marieIncrementalSyncBody = string(body)
	return nil
}

// theIncrementalSyncContainsRoomInJoin asserts that lastRoomID is in rooms.join.
func theIncrementalSyncContainsRoomInJoin() error {
	var syncResp struct {
		Rooms struct {
			Join map[string]json.RawMessage `json:"join"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal([]byte(marieIncrementalSyncBody), &syncResp); err != nil {
		return fmt.Errorf("parsing incremental sync: %w (body: %s)", err, marieIncrementalSyncBody)
	}
	if _, ok := syncResp.Rooms.Join[lastRoomID]; !ok {
		return fmt.Errorf("room %q not found in rooms.join — GAP-1 not fixed.\nSync body: %s",
			lastRoomID, marieIncrementalSyncBody)
	}
	return nil
}

// theIncrementalSyncDoesNotContainRoomInInvite asserts lastRoomID is absent from rooms.invite.
func theIncrementalSyncDoesNotContainRoomInInvite() error {
	var syncResp struct {
		Rooms struct {
			Invite map[string]json.RawMessage `json:"invite"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal([]byte(marieIncrementalSyncBody), &syncResp); err != nil {
		return fmt.Errorf("parsing incremental sync: %w", err)
	}
	if _, ok := syncResp.Rooms.Invite[lastRoomID]; ok {
		return fmt.Errorf("room %q still in rooms.invite after joining — should have moved to rooms.join",
			lastRoomID)
	}
	return nil
}

// initializeRoomFlowSteps registers all step definitions used by room_flow.feature.
// Called from InitializeScenario in steps_test.go.
func initializeRoomFlowSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		lastRoomID = ""
		lastEventID = ""
		// Access tokens are intentionally NOT reset between scenarios.
		// Sessions remain valid for the full suite run; re-authenticating every scenario
		// exhausts the /login rate-limit burst (10) and causes 429 failures.
		// Scenarios that need a fresh login must explicitly call the login step or clear
		// the token variable before calling an auth step.
		marieCapturedSyncToken = ""
		marieIncrementalSyncBody = ""
		kaiCapturedSyncToken = ""
		kaiIncrementalSyncBody = ""
		kaiInitialSyncBody = ""
		lastTxnID = ""
		lastSecondEventID = ""
		lastMsgBody = ""
		return ctx, nil
	})
	sc.Step(`^kai is authenticated via OIDC$`, kaiIsAuthenticated)
	sc.Step(`^alex is authenticated via OIDC$`, alexIsAuthenticated)
	sc.Step(`^marie is authenticated via OIDC$`, marieIsAuthenticated)
	sc.Step(`^kai creates a room named "([^"]*)"$`, kaiCreatesARoom)
	sc.Step(`^alex creates a room named "([^"]*)"$`, alexCreatesARoom)
	sc.Step(`^kai invites alex to the room$`, kaiInvitesAlex)
	sc.Step(`^alex invites marie to the room$`, alexInvitesMarie)
	sc.Step(`^alex joins the room$`, alexJoinsTheRoom)
	sc.Step(`^marie captures a sync token before joining$`, marieCapturesSyncTokenBeforeJoining)
	sc.Step(`^marie joins the room$`, marieJoinsTheRoom)
	sc.Step(`^marie calls incremental sync with the captured token$`, marieCallsIncrementalSyncWithCapturedToken)
	sc.Step(`^the incremental sync response contains the room in rooms\.join$`, theIncrementalSyncContainsRoomInJoin)
	sc.Step(`^the incremental sync response does not contain the room in rooms\.invite$`, theIncrementalSyncDoesNotContainRoomInInvite)
	sc.Step(`^kai sends the message "([^"]*)" to the room$`, kaiSendsMessage)
	sc.Step(`^kai sends the same message again with the same txnId$`, kaiSendsTheSameMessageAgain)
	sc.Step(`^alex retrieves messages from the room$`, alexRetrievesMessagesFromTheRoom)
	sc.Step(`^kai retrieves messages from the room$`, kaiRetrievesMessagesFromTheRoom)
	sc.Step(`^both sends returned the same event_id$`, bothSendsReturnedTheSameEventID)
	sc.Step(`^the response body contains "([^"]*)" exactly once$`, theBodyContainsExactlyOnce)
}
