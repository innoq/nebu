//go:build integration

package integration_test

// ─── Story 7-26: Device Management API ───────────────────────────────────────
//
// Godog step definitions for gateway/features/devices.feature.
// Written FIRST (ATDD gate) — all steps call endpoints that don't fully exist yet
// (only a GET /devices stub returning {"devices":[]} exists).
//
// State shared from room_flow_steps_test.go:
//   - kaiAccessToken, kaiUserID — set by kaiIsAuthenticated
//   - lastStatusCode, lastBody — from steps_test.go
//
// Local state:
//   - lastDeviceID — the device_id discovered from GET /devices

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/cucumber/godog"
)

// lastDeviceID holds the device_id discovered from listing kai's devices.
var lastDeviceID string

// ─── Step implementations ─────────────────────────────────────────────────────

// kaiHasAKnownDeviceID discovers kai's device_id from GET /devices.
// Used as a "Given" step to populate lastDeviceID before subsequent steps.
func kaiHasAKnownDeviceID() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/devices", matrixURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /devices request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /devices failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /devices: expected 200, got %d; body: %s", resp.StatusCode, body)
	}

	var result struct {
		Devices []struct {
			DeviceID string `json:"device_id"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parsing GET /devices response: %w (body: %s)", err, body)
	}
	if len(result.Devices) == 0 {
		return fmt.Errorf("GET /devices: no devices found for kai (empty list)")
	}
	lastDeviceID = result.Devices[0].DeviceID
	return nil
}

// kaiCallsGetDevices calls GET /_matrix/client/v3/devices authenticated as kai.
func kaiCallsGetDevices() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/devices", matrixURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /devices request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /devices failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsGetDeviceByID calls GET /_matrix/client/v3/devices/{lastDeviceID}.
func kaiCallsGetDeviceByID() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/devices/%s", matrixURL, lastDeviceID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /devices/%s request: %w", lastDeviceID, err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /devices/%s failed: %w", lastDeviceID, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsGetDeviceByUnknownID calls GET /_matrix/client/v3/devices/UNKNOWN_DEVICE_XYZ_999.
func kaiCallsGetDeviceByUnknownID() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/devices/UNKNOWN_DEVICE_XYZ_999", matrixURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /devices/UNKNOWN request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /devices/UNKNOWN failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsPutDeviceWithBody calls PUT /_matrix/client/v3/devices/{lastDeviceID} with the given body.
func kaiCallsPutDeviceWithBody(bodyStr string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/devices/%s", matrixURL, lastDeviceID)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBufferString(bodyStr))
	if err != nil {
		return fmt.Errorf("building PUT /devices/%s request: %w", lastDeviceID, err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /devices/%s failed: %w", lastDeviceID, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsPutUnknownDeviceWithBody calls PUT /_matrix/client/v3/devices/UNKNOWN_DEVICE_XYZ_999.
func kaiCallsPutUnknownDeviceWithBody(bodyStr string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/devices/UNKNOWN_DEVICE_XYZ_999", matrixURL)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBufferString(bodyStr))
	if err != nil {
		return fmt.Errorf("building PUT /devices/UNKNOWN request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /devices/UNKNOWN failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsDeleteDeviceWithNoBody calls DELETE /_matrix/client/v3/devices/{lastDeviceID}
// without any request body (should trigger UIA challenge).
func kaiCallsDeleteDeviceWithNoBody() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/devices/%s", matrixURL, lastDeviceID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("building DELETE /devices/%s request: %w", lastDeviceID, err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE /devices/%s failed: %w", lastDeviceID, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// kaiCallsDeleteDeviceWithCompletedUIA calls DELETE /_matrix/client/v3/devices/{lastDeviceID}
// with a completed UIA auth body. First gets UIA session from 401 challenge, then retries.
func kaiCallsDeleteDeviceWithCompletedUIA() error {
	// Step 1: Trigger UIA challenge to get session ID.
	url := fmt.Sprintf("%s/_matrix/client/v3/devices/%s", matrixURL, lastDeviceID)
	req1, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("building DELETE /devices request: %w", err)
	}
	req1.Header.Set("Authorization", "Bearer "+kaiAccessToken)

	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		return fmt.Errorf("DELETE /devices (step 1) failed: %w", err)
	}
	defer resp1.Body.Close()
	body1, _ := io.ReadAll(resp1.Body)

	if resp1.StatusCode != http.StatusUnauthorized {
		lastStatusCode = resp1.StatusCode
		lastBody = string(body1)
		return fmt.Errorf("expected 401 UIA challenge, got %d", resp1.StatusCode)
	}

	// Extract UIA session ID from challenge body.
	var challenge struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(body1, &challenge); err != nil {
		return fmt.Errorf("parsing UIA challenge: %w (body: %s)", err, body1)
	}
	if challenge.Session == "" {
		return fmt.Errorf("no session in UIA challenge body: %s", body1)
	}

	// Step 2: For integration tests, UIA requires real OIDC re-auth.
	// Since MVP UIA flow is not yet fully integrated with Dex in tests,
	// we send the auth body with the session. The server may still return
	// 401 or 403 depending on whether it verifies the OIDC callback.
	// This step tests that the server processes the auth body (not ignores it).
	authBody := fmt.Sprintf(`{"auth":{"type":"m.login.sso","session":"%s"}}`, challenge.Session)
	req2, err := http.NewRequest(http.MethodDelete, url, bytes.NewBufferString(authBody))
	if err != nil {
		return fmt.Errorf("building DELETE /devices (step 2) request: %w", err)
	}
	req2.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	req2.Header.Set("Content-Type", "application/json")

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return fmt.Errorf("DELETE /devices (step 2) failed: %w", err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	lastStatusCode = resp2.StatusCode
	lastBody = string(body2)
	return nil
}

// unauthenticatedClientCallsGetDevices calls GET /devices without any Authorization header.
func unauthenticatedClientCallsGetDevices() error {
	url := fmt.Sprintf("%s/_matrix/client/v3/devices", matrixURL)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building unauthenticated GET /devices request: %w", err)
	}
	// Deliberately no Authorization header.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("unauthenticated GET /devices failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// ─── Assertion helpers ────────────────────────────────────────────────────────
// theResponseBodyContains is re-used from steps_test.go (no new assertions needed here).

// ─── Step registration ────────────────────────────────────────────────────────

// initializeDevicesSteps registers all step definitions for devices.feature.
// Called from InitializeScenario in steps_test.go.
func initializeDevicesSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai has a known device ID$`, kaiHasAKnownDeviceID)
	sc.Step(`^kai calls GET /_matrix/client/v3/devices$`, kaiCallsGetDevices)
	sc.Step(`^kai calls GET /_matrix/client/v3/devices/\{deviceId\}$`, kaiCallsGetDeviceByID)
	sc.Step(`^kai calls GET /_matrix/client/v3/devices/UNKNOWN_DEVICE_XYZ_999$`, kaiCallsGetDeviceByUnknownID)
	sc.Step(`^kai calls PUT /_matrix/client/v3/devices/\{deviceId\} with body (.+)$`, kaiCallsPutDeviceWithBody)
	sc.Step(`^kai calls PUT /_matrix/client/v3/devices/UNKNOWN_DEVICE_XYZ_999 with body (.+)$`, kaiCallsPutUnknownDeviceWithBody)
	sc.Step(`^kai calls DELETE /_matrix/client/v3/devices/\{deviceId\} with no body$`, kaiCallsDeleteDeviceWithNoBody)
	sc.Step(`^kai calls DELETE /_matrix/client/v3/devices/\{deviceId\} with completed UIA$`, kaiCallsDeleteDeviceWithCompletedUIA)
	sc.Step(`^an unauthenticated client calls GET /_matrix/client/v3/devices$`, unauthenticatedClientCallsGetDevices)
	// Note: ^the response body is "([^"]*)"$  is already registered by initializeTagsSteps.
}
