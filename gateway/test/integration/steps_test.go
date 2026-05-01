//go:build integration

package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// lastStatusCode, lastBody, and lastLocationHeader hold the most recent HTTP response.
// Scenarios run sequentially in godog — no concurrency concern.
var lastStatusCode int
var lastBody string
var lastLocationHeader string

// theDockerComposeStackIsStarted is a no-op: make test-integration runs
// `docker compose up -d --wait` before `go test`, so the stack is always up.
func theDockerComposeStackIsStarted() error {
	return nil
}

// iCallGETOnGateway makes a GET request to gatewayURL+path and stores the response.
// Matches steps: "I call GET /health on the gateway", "I call GET /ready on the gateway"
func iCallGETOnGateway(path string) error {
	url := gatewayURL + path
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body from %s: %w", url, err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// iCallGETOnCore makes a GET request to coreURL+path and stores the response.
// Matches step: "I call GET :4000/health on the core" (captures "/health" from ":4000/health")
func iCallGETOnCore(path string) error {
	url := coreURL + path
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading body from %s: %w", url, err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// theResponseStatusIs asserts the last response had the expected HTTP status code.
func theResponseStatusIs(expected int) error {
	if lastStatusCode != expected {
		return fmt.Errorf("expected HTTP %d, got %d (body: %s)", expected, lastStatusCode, lastBody)
	}
	return nil
}

// theResponseBodyContains asserts the last response body contains the expected substring.
func theResponseBodyContains(expected string) error {
	if !strings.Contains(lastBody, expected) {
		return fmt.Errorf("expected body to contain %q, got: %s", expected, lastBody)
	}
	return nil
}

// captureResponse reads the response body and stores it in lastStatusCode / lastBody.
func captureResponse(resp *http.Response) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// InitializeScenario registers all step definitions for the integration test suite.
func InitializeScenario(sc *godog.ScenarioContext) {
	sc.Step(`^the docker compose stack is started$`, theDockerComposeStackIsStarted)
	sc.Step(`^I call GET (/\S+) on the gateway$`, iCallGETOnGateway)
	sc.Step(`^I call GET :4000(/\S+) on the core$`, iCallGETOnCore)
	sc.Step(`^the response status is (\d+)$`, theResponseStatusIs)
	sc.Step(`^the response body contains "([^"]*)"$`, theResponseBodyContains)
	initializeAuthSteps(sc)            // auth scenario step definitions
	initializeAdminBootstrapSteps(sc)  // admin bootstrap + dashboard step definitions
	initializeRoomFlowSteps(sc)        // room flow step definitions
	initializeComplianceFlowSteps(sc)  // compliance flow step definitions (Story 5.9)
	initializeRoomStateSteps(sc)         // room state API step definitions (Story 7-19)
	initializeJoinedMembersSteps(sc)     // joined members API step definitions (Story 7-20)
	initializeProfileSubfieldSteps(sc)   // profile sub-field API step definitions (Story 7-21)
	initializeModerationSteps(sc)        // room moderation step definitions (Story 7-22)
	initializeRoomAliasesSteps(sc)       // room aliases API step definitions (Story 7-23)
	initializeAccountDataSteps(sc)       // account data API step definitions (Story 7-24)
	initializeTagsSteps(sc)              // tags API step definitions (Story 7-25)
	initializeDevicesSteps(sc)           // device management step definitions (Story 7-26)
	initializePublicRoomsSteps(sc)       // public room directory step definitions (Story 7-27)
	initializeEventContextSteps(sc)      // event context step definitions (Story 7-28)
	initializeNotificationsSteps(sc)     // notifications API step definitions (Story 7-29)
	initializePushRulesSteps(sc)          // push rules + pushers API step definitions (Story 7-30)
	initializeAdminAPISteps(sc)           // Admin API step definitions (Story 6-11)
}
