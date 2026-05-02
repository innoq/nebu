//go:build integration

package integration_test

// ─── Story 7-21: GET /_matrix/client/v3/profile/{userId}/displayname + /avatar_url ─
//
// Godog step definitions for gateway/features/profile_subfields.feature.
// Written FIRST (ATDD gate) — all steps call endpoints that don't exist yet.
// The routes are not registered in main.go → every HTTP call returns 404 until
// GetDisplayname and GetAvatarURL handlers are wired.
//
// State: kaiUserID, kaiAccessToken are populated by kaiIsAuthenticated
// (defined in room_flow_steps_test.go). matrixURL is from main_test.go.
// lastStatusCode, lastBody are from steps_test.go.

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// kaiSetsAvatarURLTo issues PUT /_matrix/client/v3/profile/{kaiUserID}/avatar_url
// authenticated as kai, populating Kai's avatar_url so that subsequent
// GET /profile/{userId}/avatar_url assertions have a known value.
// Used by Background to set up profile state for AC2 scenarios.
func kaiSetsAvatarURLTo(avatarURL string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/profile/%s/avatar_url", matrixURL, kaiUserID)
	body := fmt.Sprintf(`{"avatar_url":%q}`, avatarURL)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("building PUT /profile/avatar_url request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /profile/avatar_url failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PUT /profile/avatar_url expected 200, got %d (body: %s)", resp.StatusCode, string(respBody))
	}
	return nil
}

// unauthenticatedClientCallsGetProfileDisplayname calls
// GET /_matrix/client/v3/profile/{userId}/displayname without any Authorization header.
// The endpoint is public (no JWT required per Matrix spec and AC4).
// Stores status + body in lastStatusCode/lastBody.
func unauthenticatedClientCallsGetProfileDisplayname(userID string) error {
	// Resolve {kaiUserId} placeholder.
	if userID == "{kaiUserId}" {
		userID = kaiUserID
	}
	url := fmt.Sprintf("%s/_matrix/client/v3/profile/%s/displayname", matrixURL, userID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /profile/displayname request: %w", err)
	}
	// Deliberately no Authorization header — endpoint is unauthenticated.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /profile/displayname failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// unauthenticatedClientCallsGetProfileAvatarURL calls
// GET /_matrix/client/v3/profile/{userId}/avatar_url without any Authorization header.
// The endpoint is public (no JWT required per Matrix spec and AC4).
// Stores status + body in lastStatusCode/lastBody.
func unauthenticatedClientCallsGetProfileAvatarURL(userID string) error {
	// Resolve {kaiUserId} placeholder.
	if userID == "{kaiUserId}" {
		userID = kaiUserID
	}
	url := fmt.Sprintf("%s/_matrix/client/v3/profile/%s/avatar_url", matrixURL, userID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /profile/avatar_url request: %w", err)
	}
	// Deliberately no Authorization header — endpoint is unauthenticated.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /profile/avatar_url failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)
	return nil
}

// theResponseBodyDoesNotContain asserts that the last response body does NOT contain
// the given substring. Used to verify that sub-field endpoints return only one field.
func theResponseBodyDoesNotContain(unexpected string) error {
	if strings.Contains(lastBody, unexpected) {
		return fmt.Errorf("expected body NOT to contain %q, but it did; body: %s", unexpected, lastBody)
	}
	return nil
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeProfileSubfieldSteps registers all step definitions for profile_subfields.feature.
// Called from InitializeScenario in steps_test.go.
func initializeProfileSubfieldSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai sets his avatar_url to "([^"]*)"$`, kaiSetsAvatarURLTo)
	sc.Step(`^an unauthenticated client calls GET /profile/([^/]+)/displayname$`, unauthenticatedClientCallsGetProfileDisplayname)
	sc.Step(`^an unauthenticated client calls GET /profile/([^/]+)/avatar_url$`, unauthenticatedClientCallsGetProfileAvatarURL)
	sc.Step(`^the response body does not contain "([^"]*)"$`, theResponseBodyDoesNotContain)
}
