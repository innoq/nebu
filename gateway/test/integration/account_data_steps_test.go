//go:build integration

package integration_test

// ─── Story 7-24: Account Data API ────────────────────────────────────────────
//
// Godog step definitions for gateway/features/account_data.feature.
// Written FIRST (ATDD gate) — all steps call endpoints that may not yet be
// fully implemented (the existing stubs return 404/empty {} without persistence).
//
// State shared from room_flow_steps_test.go:
//   - kaiAccessToken, kaiUserID — set by kaiIsAuthenticated
//   - alexAccessToken, alexUserID — set by alexIsAuthenticated
//   - lastRoomID — set by kaiCreatesARoom
//   - lastStatusCode, lastBody — from steps_test.go

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// ─── PUT helpers ─────────────────────────────────────────────────────────────

// kaiPutsRoomAccountData calls
// PUT /_matrix/client/v3/user/{kaiUserID}/rooms/{lastRoomID}/account_data/{eventType}
// with the given JSON body, authenticated as kai.
func kaiPutsRoomAccountData(eventType, body string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/user/%s/rooms/%s/account_data/%s",
		matrixURL, kaiUserID, lastRoomID, eventType)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("building PUT room account_data request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT room account_data failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// kaiGetsRoomAccountData calls
// GET /_matrix/client/v3/user/{kaiUserID}/rooms/{lastRoomID}/account_data/{eventType}
// authenticated as kai.
func kaiGetsRoomAccountData(eventType string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/user/%s/rooms/%s/account_data/%s",
		matrixURL, kaiUserID, lastRoomID, eventType)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET room account_data request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET room account_data failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// alexPutsRoomAccountDataForUser calls
// PUT /_matrix/client/v3/user/{targetUserID}/rooms/{lastRoomID}/account_data/{eventType}
// authenticated as alex. Used to test AC3 (userId mismatch → 403).
func alexPutsRoomAccountDataForUser(eventType, body, targetUserID string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/user/%s/rooms/%s/account_data/%s",
		matrixURL, targetUserID, lastRoomID, eventType)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("building PUT room account_data (mismatch) request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+alexAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT room account_data (mismatch) failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// kaiPutsGlobalAccountData calls
// PUT /_matrix/client/v3/user/{kaiUserID}/account_data/{eventType}
// authenticated as kai.
func kaiPutsGlobalAccountData(eventType, body string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/user/%s/account_data/%s",
		matrixURL, kaiUserID, eventType)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("building PUT global account_data request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT global account_data failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// kaiGetsGlobalAccountData calls
// GET /_matrix/client/v3/user/{kaiUserID}/account_data/{eventType}
// authenticated as kai.
func kaiGetsGlobalAccountData(eventType string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/user/%s/account_data/%s",
		matrixURL, kaiUserID, eventType)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET global account_data request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET global account_data failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// ─── Assertion helpers ────────────────────────────────────────────────────────

// accountDataResponseBodyIs asserts the last response body equals the expected string (trimmed).
func accountDataResponseBodyIs(expected string) error {
	got := strings.TrimSpace(lastBody)
	if got != expected {
		return fmt.Errorf("expected body %q, got %q", expected, got)
	}
	return nil
}

// ─── Sync helpers (shared by account_data and tags scenarios) ─────────────────

// kaiCapturesSyncTokenBeforeAccountDataChange performs GET /sync?timeout=0 as kai
// and stores next_batch in kaiCapturedSyncToken for use in incremental sync assertions.
func kaiCapturesSyncTokenBeforeAccountDataChange() error {
	req, err := http.NewRequest(http.MethodGet,
		matrixURL+"/_matrix/client/v3/sync?timeout=0", nil)
	if err != nil {
		return fmt.Errorf("building sync request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /sync (pre-change): %w", err)
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
		return fmt.Errorf("no next_batch in pre-change sync: %s", string(body))
	}
	kaiCapturedSyncToken = syncResp.NextBatch
	return nil
}

// kaiCapturesSyncTokenBeforeTagChange is the same operation, but named for the
// tags.feature scenario step text. Both store the result in kaiCapturedSyncToken.
func kaiCapturesSyncTokenBeforeTagChange() error {
	return kaiCapturesSyncTokenBeforeAccountDataChange()
}

// kaiCallsIncrementalSyncWithCapturedToken calls GET /sync?since=<kaiCapturedSyncToken>
// as kai, retrying up to 3 times with 500ms delay (sync processing is async).
// The response body is stored in kaiIncrementalSyncBody.
func kaiCallsIncrementalSyncWithCapturedToken() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/sync?since=%s&timeout=0",
		matrixURL, kaiCapturedSyncToken)
	var body []byte
	var statusCode int
	for i := 0; i < 3; i++ {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("building incremental sync request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
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
	kaiIncrementalSyncBody = string(body)
	return nil
}

// theIncrementalSyncContainsAccountDataEventOfType asserts that kaiIncrementalSyncBody
// contains an entry in rooms.join.<lastRoomID>.account_data.events with the given type.
func theIncrementalSyncContainsAccountDataEventOfType(eventType string) error {
	var syncResp struct {
		Rooms struct {
			Join map[string]struct {
				AccountData struct {
					Events []struct {
						Type string `json:"type"`
					} `json:"events"`
				} `json:"account_data"`
			} `json:"join"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal([]byte(kaiIncrementalSyncBody), &syncResp); err != nil {
		return fmt.Errorf("parsing incremental sync body: %w (body: %s)", err, kaiIncrementalSyncBody)
	}
	roomData, ok := syncResp.Rooms.Join[lastRoomID]
	if !ok {
		return fmt.Errorf("room %q not found in rooms.join — sync propagation not working.\nSync body: %s",
			lastRoomID, kaiIncrementalSyncBody)
	}
	for _, event := range roomData.AccountData.Events {
		if event.Type == eventType {
			return nil
		}
	}
	return fmt.Errorf("account_data event of type %q not found in rooms.join.%s.account_data.events.\nSync body: %s",
		eventType, lastRoomID, kaiIncrementalSyncBody)
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeAccountDataSteps registers all step definitions for account_data.feature.
// Called from InitializeScenario in steps_test.go.
func initializeAccountDataSteps(sc *godog.ScenarioContext) {
	// PUT per-room: body is a JSON object — use (.+?) to match nested braces correctly.
	sc.Step(`^kai puts room account data type "([^"]*)" with body (.+?) for the created room$`, kaiPutsRoomAccountData)
	// GET per-room
	sc.Step(`^kai gets room account data type "([^"]*)" for the created room$`, kaiGetsRoomAccountData)
	// PUT per-room as alex for a different user: AC3 (userId mismatch)
	sc.Step(`^alex puts room account data type "([^"]*)" with body (.+?) for user "([^"]*)" in the created room$`, alexPutsRoomAccountDataForUser)
	// PUT global: body is a JSON object
	sc.Step(`^kai puts global account data type "([^"]*)" with body (.+)$`, kaiPutsGlobalAccountData)
	// GET global
	sc.Step(`^kai gets global account data type "([^"]*)"$`, kaiGetsGlobalAccountData)
	// Assertion: response body is exactly a given string
	sc.Step(`^the response body is "([^"]*)"$`, accountDataResponseBodyIs)
	// Reuse existing "does not contain" step from profile_subfields_steps_test.go
	// (theResponseBodyDoesNotContain is already registered by initializeProfileSubfieldSteps).
	// Story 7-36: sync propagation steps
	sc.Step(`^kai captures a sync token before account data change$`, kaiCapturesSyncTokenBeforeAccountDataChange)
	sc.Step(`^kai captures a sync token before tag change$`, kaiCapturesSyncTokenBeforeTagChange)
	sc.Step(`^kai calls incremental sync with the captured token$`, kaiCallsIncrementalSyncWithCapturedToken)
	sc.Step(`^the incremental sync contains account_data event of type "([^"]*)" for the room$`, theIncrementalSyncContainsAccountDataEventOfType)
}
