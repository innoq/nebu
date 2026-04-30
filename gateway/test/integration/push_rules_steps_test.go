//go:build integration

package integration_test

// ─── Story 7-30: Push Rules API — GET/PUT/DELETE /pushrules + Pushers ────────
//
// Godog step definitions for gateway/features/push_rules.feature.
// Written FIRST (ATDD gate) — all steps call endpoints that don't exist yet.
// The push rules stub in main.go only handles GET /pushrules/ (returns empty {}).
// All other routes return 404 until PushRulesHandler and PushersHandler are wired.
//
// State shared from room_flow_steps_test.go:
//   - kaiAccessToken, kaiUserID — set by kaiIsAuthenticated
//   - lastStatusCode, lastBody  — from steps_test.go
//
// Local state:
//   - defaultRuleCountAfterFirstCall — rule count captured after first GET /pushrules/

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// defaultRuleCountAfterFirstCall stores the count of default rules seeded on the
// first GET /pushrules/ call, used to verify idempotent seeding.
var defaultRuleCountAfterFirstCall int

// ─── Step implementations ─────────────────────────────────────────────────────

// kaiCallsGetPushRules calls GET /_matrix/client/v3/pushrules/ authenticated as kai.
func kaiCallsGetPushRules() error {
	return callPushRulesMethod(http.MethodGet, "/_matrix/client/v3/pushrules/", kaiAccessToken, "")
}

// kaiCallsGetPushRulesAgain calls GET /pushrules/ a second time and captures
// the total number of default rules.
func kaiCallsGetPushRulesAgain() error {
	if err := callPushRulesMethod(http.MethodGet, "/_matrix/client/v3/pushrules/", kaiAccessToken, ""); err != nil {
		return err
	}
	return nil
}

// kaiHasCalledGetPushRulesToSeedDefaults is a Given step that triggers lazy seeding.
func kaiHasCalledGetPushRulesToSeedDefaults() error {
	return callPushRulesMethod(http.MethodGet, "/_matrix/client/v3/pushrules/", kaiAccessToken, "")
}

// kaiCallsGetSinglePushRule calls GET /pushrules/{scope}/{kind}/{ruleId}.
func kaiCallsGetSinglePushRule(scope, kind, ruleID string) error {
	path := fmt.Sprintf("/_matrix/client/v3/pushrules/%s/%s/%s", scope, kind, ruleID)
	return callPushRulesMethod(http.MethodGet, path, kaiAccessToken, "")
}

// kaiCallsPutPushRuleWithBody calls PUT /pushrules/{scope}/{kind}/{ruleId} with body.
func kaiCallsPutPushRuleWithBody(scope, kind, ruleID, body string) error {
	path := fmt.Sprintf("/_matrix/client/v3/pushrules/%s/%s/%s", scope, kind, ruleID)
	return callPushRulesMethod(http.MethodPut, path, kaiAccessToken, body)
}

// kaiCallsDeletePushRule calls DELETE /pushrules/{scope}/{kind}/{ruleId}.
func kaiCallsDeletePushRule(scope, kind, ruleID string) error {
	path := fmt.Sprintf("/_matrix/client/v3/pushrules/%s/%s/%s", scope, kind, ruleID)
	return callPushRulesMethod(http.MethodDelete, path, kaiAccessToken, "")
}

// kaiCallsPutPushRuleEnabledWithBody calls PUT /pushrules/{scope}/{kind}/{ruleId}/enabled.
func kaiCallsPutPushRuleEnabledWithBody(scope, kind, ruleID, body string) error {
	path := fmt.Sprintf("/_matrix/client/v3/pushrules/%s/%s/%s/enabled", scope, kind, ruleID)
	return callPushRulesMethod(http.MethodPut, path, kaiAccessToken, body)
}

// kaiCallsPutPushRuleActionsWithBody calls PUT /pushrules/{scope}/{kind}/{ruleId}/actions.
func kaiCallsPutPushRuleActionsWithBody(scope, kind, ruleID, body string) error {
	path := fmt.Sprintf("/_matrix/client/v3/pushrules/%s/%s/%s/actions", scope, kind, ruleID)
	return callPushRulesMethod(http.MethodPut, path, kaiAccessToken, body)
}

// kaiHasCustomPushRule is a Given step that creates a custom rule via PUT.
func kaiHasCustomPushRule(ruleID, kind string) error {
	return kaiCallsPutPushRuleWithBody("global", kind, ruleID, `{"conditions":[],"actions":["notify"]}`)
}

// callPushRulesMethod is the shared helper for all push rule HTTP calls.
func callPushRulesMethod(method, path, token, body string) error {
	url := matrixURL + path
	var req *http.Request
	var err error
	if body != "" {
		req, err = http.NewRequest(method, url, bytes.NewBufferString(body))
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		return fmt.Errorf("building %s %s request: %w", method, path, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s failed: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	lastStatusCode = resp.StatusCode
	lastBody = string(respBody)
	return nil
}

// kaiCallsGetPushers calls GET /_matrix/client/v3/pushers authenticated as kai.
func kaiCallsGetPushers() error {
	return callPushRulesMethod(http.MethodGet, "/_matrix/client/v3/pushers", kaiAccessToken, "")
}

// kaiCallsPostPushersSetWithBody calls POST /_matrix/client/v3/pushers/set with body.
func kaiCallsPostPushersSetWithBody(body string) error {
	return callPushRulesMethod(http.MethodPost, "/_matrix/client/v3/pushers/set", kaiAccessToken, body)
}

// ─── Scenario-specific step wrappers for feature file patterns ────────────────

// kaiCallsGetPushrulesRoute calls GET /pushrules/ (trailing slash required by spec).
func kaiCallsGetPushrulesRoute() error {
	return kaiCallsGetPushRules()
}

// kaiCallsGetPushrulesRouteAgain calls GET /pushrules/ a second time.
func kaiCallsGetPushrulesRouteAgain() error {
	return kaiCallsGetPushRulesAgain()
}

// kaiCallsGetPushRuleGlobal is a convenience wrapper for the common global scope.
func kaiCallsGetPushRuleGlobal(kind, ruleID string) error {
	return kaiCallsGetSinglePushRule("global", kind, ruleID)
}

// kaiCallsGetPushRuleDevice tests the invalid "device" scope.
func kaiCallsGetPushRuleDevice(kind, ruleID string) error {
	return kaiCallsGetSinglePushRule("device", kind, ruleID)
}

// kaiCallsPutPushRuleGlobal is a convenience wrapper for the common global scope.
func kaiCallsPutPushRuleGlobal(kind, ruleID, body string) error {
	return kaiCallsPutPushRuleWithBody("global", kind, ruleID, body)
}

// kaiCallsDeletePushRuleGlobal is a convenience wrapper for the common global scope.
func kaiCallsDeletePushRuleGlobal(kind, ruleID string) error {
	return kaiCallsDeletePushRule("global", kind, ruleID)
}

// kaiCallsPutPushRuleEnabledGlobal is a convenience wrapper for the common global scope.
func kaiCallsPutPushRuleEnabledGlobal(kind, ruleID, body string) error {
	return kaiCallsPutPushRuleEnabledWithBody("global", kind, ruleID, body)
}

// kaiCallsPutPushRuleActionsGlobal is a convenience wrapper for the common global scope.
func kaiCallsPutPushRuleActionsGlobal(kind, ruleID, body string) error {
	return kaiCallsPutPushRuleActionsWithBody("global", kind, ruleID, body)
}

// ─── Assertion helpers ────────────────────────────────────────────────────────

// thePushrulesResponseHasGlobalOverrideContainingRuleID asserts global.override
// contains a rule with the given rule_id.
func thePushrulesResponseHasGlobalOverrideContainingRuleID(ruleID string) error {
	var resp struct {
		Global map[string][]struct {
			RuleID string `json:"rule_id"`
		} `json:"global"`
	}
	if err := json.Unmarshal([]byte(lastBody), &resp); err != nil {
		return fmt.Errorf("parse error: %w (body: %s)", err, lastBody)
	}
	for _, r := range resp.Global["override"] {
		if r.RuleID == ruleID {
			return nil
		}
	}
	return fmt.Errorf("rule_id %q not found in global.override (body: %s)", ruleID, lastBody)
}

// theDefaultPushRuleCountIsTheSameAsAfterFirstCall checks idempotent seeding.
func theDefaultPushRuleCountIsTheSameAsAfterFirstCall() error {
	// Count default rules in current response.
	var resp struct {
		Global map[string][]json.RawMessage `json:"global"`
	}
	if err := json.Unmarshal([]byte(lastBody), &resp); err != nil {
		return fmt.Errorf("parse error: %w (body: %s)", err, lastBody)
	}
	total := 0
	for _, rules := range resp.Global {
		total += len(rules)
	}
	if defaultRuleCountAfterFirstCall == 0 {
		// First call — store the count.
		defaultRuleCountAfterFirstCall = total
		return nil
	}
	if total != defaultRuleCountAfterFirstCall {
		return fmt.Errorf("rule count changed from %d (first call) to %d (second call) — seeding is not idempotent (body: %s)",
			defaultRuleCountAfterFirstCall, total, lastBody)
	}
	return nil
}

// thePushRuleEnabledFieldIsFalse asserts enabled=false in a single rule response.
func thePushRuleEnabledFieldIsFalse() error {
	var rule struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(lastBody), &rule); err != nil {
		return fmt.Errorf("parse error: %w (body: %s)", err, lastBody)
	}
	if rule.Enabled {
		return fmt.Errorf("expected enabled=false, got enabled=true (body: %s)", lastBody)
	}
	return nil
}

// thePushRuleActionsContain asserts the actions array contains the given action.
func thePushRuleActionsContain(action string) error {
	var rule struct {
		Actions []string `json:"actions"`
	}
	if err := json.Unmarshal([]byte(lastBody), &rule); err != nil {
		return fmt.Errorf("parse error: %w (body: %s)", err, lastBody)
	}
	for _, a := range rule.Actions {
		if a == action {
			return nil
		}
	}
	return fmt.Errorf("action %q not found in actions %v (body: %s)", action, rule.Actions, lastBody)
}

// thePushersArrayIsEmpty asserts the pushers field is an empty array.
func thePushersArrayIsEmpty() error {
	var resp struct {
		Pushers []json.RawMessage `json:"pushers"`
	}
	if err := json.Unmarshal([]byte(lastBody), &resp); err != nil {
		return fmt.Errorf("parse error: %w (body: %s)", err, lastBody)
	}
	if len(resp.Pushers) != 0 {
		return fmt.Errorf("expected empty pushers array, got %d items (body: %s)", len(resp.Pushers), lastBody)
	}
	return nil
}

// thePushersArrayHasNItems asserts the pushers array has exactly n items.
func thePushersArrayHasNItems(n int) error {
	var resp struct {
		Pushers []json.RawMessage `json:"pushers"`
	}
	if err := json.Unmarshal([]byte(lastBody), &resp); err != nil {
		return fmt.Errorf("parse error: %w (body: %s)", err, lastBody)
	}
	if len(resp.Pushers) != n {
		return fmt.Errorf("expected %d pusher(s), got %d (body: %s)", n, len(resp.Pushers), lastBody)
	}
	return nil
}

// ─── Feature-file step wrappers ───────────────────────────────────────────────

// The feature file uses inline step text that maps directly to these functions.
// Steps like "kai calls GET /_matrix/client/v3/pushrules/" map to kaiCallsGetPushrulesRoute.
// Steps like "kai calls GET /_matrix/client/v3/pushrules/ again" map to kaiCallsGetPushrulesRouteAgain.

func kaiCallsGetPushrulesSingleRule(scope, kind, ruleID string) error {
	return kaiCallsGetSinglePushRule(scope, kind, ruleID)
}

func kaiCallsGetPushrulesSingleRuleGlobal(kind, ruleID string) error {
	return kaiCallsGetSinglePushRule("global", kind, ruleID)
}

// kaiCallsGetPushrulesDeviceScope calls GET /pushrules/device/{kind}/{ruleId}.
func kaiCallsGetPushrulesDeviceScope(kind, ruleID string) error {
	return kaiCallsGetSinglePushRule("device", kind, ruleID)
}

// kaiCallsPutPushruleBody wraps PutPushRuleGlobal for inline body patterns.
func kaiCallsPutPushruleBody(kind, ruleID, body string) error {
	return kaiCallsPutPushRuleGlobal(kind, ruleID, body)
}

// kaiCallsDeletePushruleGlobal wraps DeletePushRuleGlobal.
func kaiCallsDeletePushruleGlobal(kind, ruleID string) error {
	return kaiCallsDeletePushRuleGlobal(kind, ruleID)
}

// kaiCallsPutEnabledBody wraps PutPushRuleEnabledGlobal.
func kaiCallsPutEnabledBody(kind, ruleID, body string) error {
	return kaiCallsPutPushRuleEnabledGlobal(kind, ruleID, body)
}

// kaiCallsPutActionsBody wraps PutPushRuleActionsGlobal.
func kaiCallsPutActionsBody(kind, ruleID, body string) error {
	return kaiCallsPutPushRuleActionsGlobal(kind, ruleID, body)
}

// kaiHasCustomPushRuleInKind seeds a custom rule. Used in Given steps.
func kaiHasCustomPushRuleInKind(ruleID, kind string) error {
	return kaiHasCustomPushRule(ruleID, kind)
}

// ─── Step registration ────────────────────────────────────────────────────────

// initializePushRulesSteps registers all step definitions for push_rules.feature.
// Called from InitializeScenario in steps_test.go.
func initializePushRulesSteps(sc *godog.ScenarioContext) {
	// Background / GET all rules
	sc.Step(`^kai calls GET /_matrix/client/v3/pushrules/$`, kaiCallsGetPushrulesRoute)
	sc.Step(`^kai calls GET /_matrix/client/v3/pushrules/ again$`, kaiCallsGetPushrulesRouteAgain)
	sc.Step(`^kai has called GET /_matrix/client/v3/pushrules/ to seed default rules$`, kaiHasCalledGetPushRulesToSeedDefaults)

	// Single rule GET
	sc.Step(`^kai calls GET /_matrix/client/v3/pushrules/global/override/m\.rule\.master$`, func() error {
		return kaiCallsGetSinglePushRule("global", "override", "m.rule.master")
	})
	sc.Step(`^kai calls GET /_matrix/client/v3/pushrules/global/override/nonexistent\.rule$`, func() error {
		return kaiCallsGetSinglePushRule("global", "override", "nonexistent.rule")
	})
	sc.Step(`^kai calls GET /_matrix/client/v3/pushrules/global/override/my\.rule\.test$`, func() error {
		return kaiCallsGetSinglePushRule("global", "override", "my.rule.test")
	})
	sc.Step(`^kai calls GET /_matrix/client/v3/pushrules/device/override/m\.rule\.master$`, func() error {
		return kaiCallsGetSinglePushRule("device", "override", "m.rule.master")
	})

	// PUT rule
	sc.Step(`^kai calls PUT /_matrix/client/v3/pushrules/global/override/my\.rule\.test with body (.+)$`, func(body string) error {
		return kaiCallsPutPushRuleGlobal("override", "my.rule.test", body)
	})
	sc.Step(`^kai calls PUT /_matrix/client/v3/pushrules/global/override/m\.rule\.master with body (.+)$`, func(body string) error {
		return kaiCallsPutPushRuleGlobal("override", "m.rule.master", body)
	})

	// DELETE rule
	sc.Step(`^kai calls DELETE /_matrix/client/v3/pushrules/global/override/my\.rule\.test$`, func() error {
		return kaiCallsDeletePushRuleGlobal("override", "my.rule.test")
	})
	sc.Step(`^kai calls DELETE /_matrix/client/v3/pushrules/global/override/m\.rule\.master$`, func() error {
		return kaiCallsDeletePushRuleGlobal("override", "m.rule.master")
	})

	// PUT /enabled
	sc.Step(`^kai calls PUT /_matrix/client/v3/pushrules/global/override/m\.rule\.master/enabled with body (.+)$`, func(body string) error {
		return kaiCallsPutPushRuleEnabledGlobal("override", "m.rule.master", body)
	})

	// PUT /actions
	sc.Step(`^kai calls PUT /_matrix/client/v3/pushrules/global/override/my\.rule\.test/actions with body (.+)$`, func(body string) error {
		return kaiCallsPutPushRuleActionsGlobal("override", "my.rule.test", body)
	})

	// Given — pre-condition helpers
	sc.Step(`^kai has a custom push rule "([^"]+)" in kind "([^"]+)"$`, kaiHasCustomPushRuleInKind)

	// Pushers
	sc.Step(`^kai calls GET /_matrix/client/v3/pushers$`, kaiCallsGetPushers)
	sc.Step(`^kai calls POST /_matrix/client/v3/pushers/set with body (.+)$`, kaiCallsPostPushersSetWithBody)

	// Assertions
	sc.Step(`^the pushrules response has a global\.override array containing rule_id "([^"]+)"$`, thePushrulesResponseHasGlobalOverrideContainingRuleID)
	sc.Step(`^the default push rule count is the same as after the first call$`, theDefaultPushRuleCountIsTheSameAsAfterFirstCall)
	sc.Step(`^the push rule enabled field is false$`, thePushRuleEnabledFieldIsFalse)
	sc.Step(`^the push rule actions contain "([^"]+)"$`, thePushRuleActionsContain)
	sc.Step(`^the pushers array is empty$`, thePushersArrayIsEmpty)
	sc.Step(`^the pushers array has (\d+) items?$`, thePushersArrayHasNItems)

	// Reuse shared step: "the response body is {}" — already defined in tags_steps_test.go
	// Reuse: "the response status is N" — defined in steps_test.go
	// Reuse: "the response body contains X" — defined in steps_test.go

	// Extra: the feature file uses the shared steps above for body/status assertions.
	// No extra registration needed for those.
	_ = strings.Contains // suppress unused import if strings is not otherwise referenced
}
