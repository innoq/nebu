//go:build integration

package integration_test

// ─── Story 11.6: Search E2E — POST /_matrix/client/v3/search ─────────────────
//
// Godog step definitions for gateway/features/search.feature.
// Written FIRST (ATDD gate) — all scenarios verify the full integration path.
//
// The POST /search endpoint is implemented in gateway/internal/matrix/search.go
// and rate-limited by NewUserRateLimiter (Story 11.5, Burst=10, 10 req/min).
//
// Shared package vars (from room_flow_steps_test.go):
//   kaiAccessToken, alexAccessToken, marieAccessToken
//   kaiUserID, alexUserID, marieUserID
//   lastRoomID, lastStatusCode, lastBody
//
// Local vars:
//   kaiPrivateSearchRoomID — kai's private room alex is NOT invited to (AC3)
//   lastSearchBody         — POST /search response (kept in sync with lastBody)
//   lastRetryAfterHeader   — Retry-After response header from most recent /search call
//
// ISOLATION CONTRACT (AC5 rate-limit scenario):
//   marieAccessToken is used exclusively for the rate-limit scenario. NO OTHER
//   scenario in this file (or any future search scenario) MUST call POST /search
//   using marieAccessToken. Violation causes the "10 consecutive requests" guard
//   to fail early with a clear error, so contamination is always detectable.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// kaiPrivateSearchRoomID holds the room kai creates for AC3 that alex is never invited to.
var kaiPrivateSearchRoomID string

// lastSearchBody stores the most recent POST /search response body.
// Kept in sync with lastBody so shared assertions (theResponseBodyContains, etc.) work.
var lastSearchBody string

// lastRetryAfterHeader stores the Retry-After HTTP header from the most recent /search call.
var lastRetryAfterHeader string

// ─── POST /search helper ──────────────────────────────────────────────────────

// callPostSearch sends POST /_matrix/client/v3/search with the given Bearer token and search term.
// Passing an empty token omits the Authorization header (unauthenticated request).
// Captures status code, body, and Retry-After header for assertions.
func callPostSearch(token, term string) error {
	body := fmt.Sprintf(`{"search_categories":{"room_events":{"search_term":%q,"order_by":"rank"}}}`, term)
	req, err := http.NewRequest(http.MethodPost,
		matrixURL+"/_matrix/client/v3/search",
		bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("building POST /search request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST /search failed: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastSearchBody = string(b)
	lastBody = lastSearchBody
	lastRetryAfterHeader = resp.Header.Get("Retry-After")
	return nil
}

// ─── Step implementations ─────────────────────────────────────────────────────

// kaiCallsPostSearchWithTerm sends a search request as kai.
func kaiCallsPostSearchWithTerm(term string) error {
	return callPostSearch(kaiAccessToken, term)
}

// alexCallsPostSearchWithTerm sends a search request as alex.
func alexCallsPostSearchWithTerm(term string) error {
	return callPostSearch(alexAccessToken, term)
}

// unauthenticatedClientCallsPostSearch sends POST /search with no Authorization header.
func unauthenticatedClientCallsPostSearch(term string) error {
	return callPostSearch("", term)
}

// kaiCreatesPrivateSearchRoomWithoutInvitingAlex creates a private room and stores its ID
// in kaiPrivateSearchRoomID. Does NOT invite alex.
func kaiCreatesPrivateSearchRoomWithoutInvitingAlex() error {
	if err := kaiCreatesARoom("search-private-room-11-6"); err != nil {
		return err
	}
	kaiPrivateSearchRoomID = lastRoomID
	return nil
}

// kaiSendsMessageToPrivateSearchRoom sends a message directly to kaiPrivateSearchRoomID.
// Does NOT touch lastRoomID (avoids global-swap anti-pattern).
func kaiSendsMessageToPrivateSearchRoom(msgBody string) error {
	if kaiPrivateSearchRoomID == "" {
		return fmt.Errorf("kaiPrivateSearchRoomID not set — kaiCreatesPrivateSearchRoomWithoutInvitingAlex must run first")
	}
	txnID := fmt.Sprintf("txn-private-%d", time.Now().UnixNano())
	payload := fmt.Sprintf(`{"msgtype":"m.text","body":%q}`, msgBody)
	url := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		matrixURL, kaiPrivateSearchRoomID, txnID)
	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building sendEvent request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+kaiAccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT /send to private room failed: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PUT /send returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// kaiCallsPostSearchWithEmptyTerm sends POST /search with search_term = "".
func kaiCallsPostSearchWithEmptyTerm() error {
	return callPostSearch(kaiAccessToken, "")
}

// theSearchResultsContainNonZeroRank checks that:
//   - search_categories.room_events.count > 0 (spec §11.14 field)
//   - at least one result entry has rank > 0
func theSearchResultsContainNonZeroRank() error {
	var parsed struct {
		SearchCategories struct {
			RoomEvents struct {
				Count   int `json:"count"`
				Results []struct {
					Rank float64 `json:"rank"`
				} `json:"results"`
			} `json:"room_events"`
		} `json:"search_categories"`
	}
	if err := json.Unmarshal([]byte(lastSearchBody), &parsed); err != nil {
		return fmt.Errorf("failed to parse search response: %w — body: %s", err, lastSearchBody)
	}
	if parsed.SearchCategories.RoomEvents.Count <= 0 {
		return fmt.Errorf("expected count > 0, got %d — body: %s", parsed.SearchCategories.RoomEvents.Count, lastSearchBody)
	}
	if len(parsed.SearchCategories.RoomEvents.Results) == 0 {
		return fmt.Errorf("expected at least one search result, got none — body: %s", lastSearchBody)
	}
	for _, r := range parsed.SearchCategories.RoomEvents.Results {
		if r.Rank > 0 {
			return nil
		}
	}
	return fmt.Errorf("no result had rank > 0 — body: %s", lastSearchBody)
}

// theSearchResultContentBodyContains checks that at least one result has
// result.content.body containing term. Validates the structured response shape
// per Matrix spec §11.14 (not just raw JSON string matching).
func theSearchResultContentBodyContains(term string) error {
	var parsed struct {
		SearchCategories struct {
			RoomEvents struct {
				Results []struct {
					Result struct {
						Content struct {
							Body string `json:"body"`
						} `json:"content"`
					} `json:"result"`
				} `json:"results"`
			} `json:"room_events"`
		} `json:"search_categories"`
	}
	if err := json.Unmarshal([]byte(lastSearchBody), &parsed); err != nil {
		return fmt.Errorf("failed to parse search response: %w — body: %s", err, lastSearchBody)
	}
	for _, r := range parsed.SearchCategories.RoomEvents.Results {
		if strings.Contains(r.Result.Content.Body, term) {
			return nil
		}
	}
	return fmt.Errorf("no result had content.body containing %q — body: %s", term, lastSearchBody)
}

// theSearchResultCountIs checks search_categories.room_events.count == expected.
func theSearchResultCountIs(expected int) error {
	var parsed struct {
		SearchCategories struct {
			RoomEvents struct {
				Count int `json:"count"`
			} `json:"room_events"`
		} `json:"search_categories"`
	}
	if err := json.Unmarshal([]byte(lastSearchBody), &parsed); err != nil {
		return fmt.Errorf("failed to parse search response: %w — body: %s", err, lastSearchBody)
	}
	if parsed.SearchCategories.RoomEvents.Count != expected {
		return fmt.Errorf("expected count %d, got %d — body: %s", expected, parsed.SearchCategories.RoomEvents.Count, lastSearchBody)
	}
	return nil
}

// theResponseHeaderIsPresent checks that the named HTTP response header is non-empty.
func theResponseHeaderIsPresent(name string) error {
	var val string
	switch strings.ToLower(name) {
	case "retry-after":
		val = lastRetryAfterHeader
	default:
		return fmt.Errorf("unsupported header assertion %q — add to theResponseHeaderIsPresent", name)
	}
	if val == "" {
		return fmt.Errorf("expected response header %q to be present and non-empty, got empty string", name)
	}
	return nil
}

// marieSends10ConsecutivePostSearchRequests sends 10 POST /search requests as marie.
// All 10 must return non-429 (bucket starts full at Burst=10 in a fresh integration stack).
// If any request returns 429 before the 10th, the bucket was pre-contaminated by another
// scenario — this is always a visible test failure, never a silent false pass.
// ISOLATION: marieAccessToken MUST NOT be used for POST /search in any other scenario.
func marieSends10ConsecutivePostSearchRequests() error {
	for i := 1; i <= 10; i++ {
		if err := callPostSearch(marieAccessToken, "rate-limit-probe-11-6"); err != nil {
			return fmt.Errorf("request %d: %w", i, err)
		}
		if lastStatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("request %d unexpectedly returned 429 — rate-limit bucket was pre-consumed (isolation violation: another scenario used marieAccessToken for POST /search)", i)
		}
	}
	return nil
}

// marieCallsPostSearchOnceMore sends one additional POST /search as marie (the 11th).
func marieCallsPostSearchOnceMore() error {
	return callPostSearch(marieAccessToken, "rate-limit-probe-11-6")
}

// ─── Step registration ────────────────────────────────────────────────────────

func initializeSearchSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai calls POST /search with term "([^"]*)"$`, kaiCallsPostSearchWithTerm)
	sc.Step(`^alex calls POST /search with term "([^"]*)"$`, alexCallsPostSearchWithTerm)
	sc.Step(`^an unauthenticated client calls POST /search with term "([^"]*)"$`, unauthenticatedClientCallsPostSearch)
	sc.Step(`^kai creates a private search room without inviting alex$`, kaiCreatesPrivateSearchRoomWithoutInvitingAlex)
	sc.Step(`^kai sends the message "([^"]*)" to the private search room$`, kaiSendsMessageToPrivateSearchRoom)
	sc.Step(`^kai calls POST /search with an empty search term$`, kaiCallsPostSearchWithEmptyTerm)
	sc.Step(`^the search results contain a non-zero rank$`, theSearchResultsContainNonZeroRank)
	sc.Step(`^the search result content body contains "([^"]*)"$`, theSearchResultContentBodyContains)
	sc.Step(`^the search result count is (\d+)$`, theSearchResultCountIs)
	sc.Step(`^the response header "([^"]*)" is present$`, theResponseHeaderIsPresent)
	sc.Step(`^marie sends 10 consecutive POST /search requests$`, marieSends10ConsecutivePostSearchRequests)
	sc.Step(`^marie sends one more POST /search request$`, marieCallsPostSearchOnceMore)
}
