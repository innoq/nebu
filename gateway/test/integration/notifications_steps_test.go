//go:build integration

package integration_test

// ─── Story 7-29: Notifications API — GET /_matrix/client/v3/notifications ──
//
// Godog step definitions for gateway/features/notifications.feature.
// Written FIRST (ATDD gate) — all steps call endpoints that don't exist yet.
// The route is not registered in main.go → every HTTP call returns 404 until
// the NotificationsHandler is wired.
//
// State shared from room_flow_steps_test.go:
//   - kaiAccessToken, kaiUserID — set by kaiIsAuthenticated
//   - lastStatusCode, lastBody — from steps_test.go
//
// Local state:
//   - lastNextToken — the next_token from the most recent /notifications response.
//
// NOTE: Scenarios that require "kai has N notifications in the database" insert
// rows directly via the migration DB connection (nebu_migrate, BYPASSRLS) so tests
// are self-contained without depending on the Event Dispatcher to write rows.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver
)

// lastNextToken stores the next_token from the most recent /notifications response.
var lastNextToken string

// ─── Step implementations ─────────────────────────────────────────────────────

// kaiCallsGetNotificationsBasic calls GET /_matrix/client/v3/notifications (no params).
func kaiCallsGetNotificationsBasic() error {
	return callGetNotifications(kaiAccessToken, "")
}

// kaiCallsGetNotificationsWithQuery calls GET /notifications?{query} authenticated as kai.
func kaiCallsGetNotificationsWithQuery(query string) error {
	return callGetNotifications(kaiAccessToken, "?"+query)
}

// callGetNotifications is the shared helper for all /notifications GET calls.
func callGetNotifications(token, queryStr string) error {
	url := fmt.Sprintf("%s/_matrix/client/v3/notifications%s", matrixURL, queryStr)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building GET /notifications request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET /notifications failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(body)

	// Extract next_token from the response if present.
	if resp.StatusCode == http.StatusOK {
		var parsed struct {
			NextToken string `json:"next_token"`
		}
		if err := json.Unmarshal(body, &parsed); err == nil {
			lastNextToken = parsed.NextToken
		}
	}

	return nil
}

// kaiHasNNotificationsInDB inserts N notification rows for kaiUserID directly into
// the database using the privileged migrationDBURL connection (nebu_migrate, BYPASSRLS).
// This is the only way to seed notifications without the Event Dispatcher.
func kaiHasNNotificationsInDB(n int) error {
	migrDB, err := sql.Open("pgx", migrationDBURL)
	if err != nil {
		return fmt.Errorf("open migration DB: %w", err)
	}
	defer migrDB.Close()

	for i := 1; i <= n; i++ {
		eventJSON := fmt.Sprintf(`{"type":"m.room.message","room_id":"!notif-room-%d:%s","event_id":"$ev%d","sender":"%s","content":{"msgtype":"m.text","body":"test %d"}}`,
			i, "test.local", i, kaiUserID, i)
		_, err := migrDB.Exec(
			`INSERT INTO notifications (user_id, room_id, event_id, event_json, actions, read)
			 VALUES ($1, $2, $3, $4, $5, false)`,
			kaiUserID,
			fmt.Sprintf("!notif-room-%d:test.local", i),
			fmt.Sprintf("$ev%d", i),
			eventJSON,
			`["notify"]`,
		)
		if err != nil {
			return fmt.Errorf("insert notification %d: %w", i, err)
		}
	}
	return nil
}

// kaiHasHighlightAndNotifyNotifications inserts 2 notifications:
// one with actions=["notify"] and one with actions=["notify","highlight"].
func kaiHasHighlightAndNotifyNotifications() error {
	migrDB, err := sql.Open("pgx", migrationDBURL)
	if err != nil {
		return fmt.Errorf("open migration DB: %w", err)
	}
	defer migrDB.Close()

	eventJSON1 := fmt.Sprintf(`{"type":"m.room.message","room_id":"!notif-hl-1:test.local","event_id":"$ev-hl-1","sender":"%s","content":{"msgtype":"m.text","body":"plain"}}`, kaiUserID)
	eventJSON2 := fmt.Sprintf(`{"type":"m.room.message","room_id":"!notif-hl-2:test.local","event_id":"$ev-hl-2","sender":"%s","content":{"msgtype":"m.text","body":"@mention"}}`, kaiUserID)

	_, err = migrDB.Exec(
		`INSERT INTO notifications (user_id, room_id, event_id, event_json, actions, read)
		 VALUES ($1, $2, $3, $4, $5, false)`,
		kaiUserID, "!notif-hl-1:test.local", "$ev-hl-1", eventJSON1, `["notify"]`,
	)
	if err != nil {
		return fmt.Errorf("insert notify notification: %w", err)
	}

	_, err = migrDB.Exec(
		`INSERT INTO notifications (user_id, room_id, event_id, event_json, actions, read)
		 VALUES ($1, $2, $3, $4, $5, false)`,
		kaiUserID, "!notif-hl-2:test.local", "$ev-hl-2", eventJSON2, `["notify","highlight"]`,
	)
	if err != nil {
		return fmt.Errorf("insert highlight notification: %w", err)
	}
	return nil
}

// kaiFetchedFirstPageWithNextToken calls GET /notifications?limit=2 and stores next_token.
func kaiFetchedFirstPageWithNextToken() error {
	return kaiCallsGetNotificationsWithQuery("limit=2")
}

// kaiCallsNotificationsWithCursorAndLimit calls GET /notifications with the stored next_token.
func kaiCallsNotificationsWithCursorAndLimit() error {
	if lastNextToken == "" {
		return fmt.Errorf("no next_token available from previous request")
	}
	query := fmt.Sprintf("from=%s&limit=2", lastNextToken)
	return kaiCallsGetNotificationsWithQuery(query)
}

// unauthenticatedCallsGetNotifications calls GET /notifications without any token.
func unauthenticatedCallsGetNotifications() error {
	return callGetNotifications("", "")
}

// ─── Assertion helpers ────────────────────────────────────────────────────────

// theNotificationsArrayIsEmpty asserts that notifications is an empty JSON array.
func theNotificationsArrayIsEmpty() error {
	var body struct {
		Notifications []json.RawMessage `json:"notifications"`
	}
	if err := json.Unmarshal([]byte(lastBody), &body); err != nil {
		return fmt.Errorf("expected JSON with 'notifications' field, parse error: %w (body: %s)", err, lastBody)
	}
	if len(body.Notifications) != 0 {
		return fmt.Errorf("expected empty notifications array, got %d items (body: %s)", len(body.Notifications), lastBody)
	}
	return nil
}

// theNextTokenIsAbsentOrEmpty asserts that next_token is absent or empty in the response.
func theNextTokenIsAbsentOrEmpty() error {
	// next_token should either not be in the JSON (omitempty) or be "".
	if strings.Contains(lastBody, `"next_token"`) {
		var body struct {
			NextToken string `json:"next_token"`
		}
		if err := json.Unmarshal([]byte(lastBody), &body); err == nil {
			if body.NextToken != "" {
				return fmt.Errorf("expected empty next_token, got %q (body: %s)", body.NextToken, lastBody)
			}
		}
	}
	return nil
}

// theNotificationsArrayHasNItems asserts the notifications array has exactly n items.
func theNotificationsArrayHasNItems(n int) error {
	var body struct {
		Notifications []json.RawMessage `json:"notifications"`
	}
	if err := json.Unmarshal([]byte(lastBody), &body); err != nil {
		return fmt.Errorf("parse error: %w (body: %s)", err, lastBody)
	}
	if len(body.Notifications) != n {
		return fmt.Errorf("expected %d notifications, got %d (body: %s)", n, len(body.Notifications), lastBody)
	}
	return nil
}

// eachNotificationItemHasRequiredKeys checks that every item in notifications has
// the keys: actions, event, read, room_id, ts.
func eachNotificationItemHasRequiredKeys() error {
	var body struct {
		Notifications []json.RawMessage `json:"notifications"`
	}
	if err := json.Unmarshal([]byte(lastBody), &body); err != nil {
		return fmt.Errorf("parse error: %w (body: %s)", err, lastBody)
	}
	for i, rawItem := range body.Notifications {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(rawItem, &item); err != nil {
			return fmt.Errorf("item[%d] is not a JSON object: %w", i, err)
		}
		for _, key := range []string{"actions", "event", "read", "room_id", "ts"} {
			if _, ok := item[key]; !ok {
				return fmt.Errorf("item[%d]: missing key %q (body: %s)", i, key, lastBody)
			}
		}
	}
	return nil
}

// theNextTokenIsPresentAndNonEmpty asserts next_token is present and non-empty.
func theNextTokenIsPresentAndNonEmpty() error {
	var body struct {
		NextToken string `json:"next_token"`
	}
	if err := json.Unmarshal([]byte(lastBody), &body); err != nil {
		return fmt.Errorf("parse error: %w (body: %s)", err, lastBody)
	}
	if body.NextToken == "" {
		return fmt.Errorf("expected non-empty next_token in response (body: %s)", lastBody)
	}
	return nil
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializeNotificationsSteps registers all step definitions for notifications.feature.
// Called from InitializeScenario in steps_test.go.
func initializeNotificationsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^kai calls GET /_matrix/client/v3/notifications$`, kaiCallsGetNotificationsBasic)
	sc.Step(`^kai calls GET /_matrix/client/v3/notifications\?(.+)$`, kaiCallsGetNotificationsWithQuery)
	sc.Step(`^kai has (\d+) notifications in the database$`, kaiHasNNotificationsInDB)
	sc.Step(`^kai has 1 notification with actions \["notify"\] and 1 with actions \["notify","highlight"\]$`, kaiHasHighlightAndNotifyNotifications)
	sc.Step(`^kai fetched the first page with limit=2 and received a next_token$`, kaiFetchedFirstPageWithNextToken)
	sc.Step(`^kai calls GET /_matrix/client/v3/notifications with that next_token and limit=2$`, kaiCallsNotificationsWithCursorAndLimit)
	sc.Step(`^an unauthenticated client calls GET /_matrix/client/v3/notifications$`, unauthenticatedCallsGetNotifications)
	sc.Step(`^the notifications array is empty$`, theNotificationsArrayIsEmpty)
	sc.Step(`^the next_token is absent or empty$`, theNextTokenIsAbsentOrEmpty)
	sc.Step(`^the notifications array has (\d+) items?$`, theNotificationsArrayHasNItems)
	sc.Step(`^each notification item has keys actions, event, read, room_id, ts$`, eachNotificationItemHasRequiredKeys)
	sc.Step(`^the next_token is present and non-empty$`, theNextTokenIsPresentAndNonEmpty)
}
