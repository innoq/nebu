Feature: Push Rules API — GET/PUT/DELETE /pushrules + Pushers
  As an end-user
  I want to manage my push notification rules via the Matrix push rules API
  So that my Matrix client can show, enable, disable, and customise notification rules
  and register push endpoints — enabling full notification control without manual server intervention

  # Story 7-30 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until the PushRulesHandler and PushersHandler are implemented
  # and routes are registered in main.go (replacing the hard-coded stub at line ~474-479).

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC

  # AC1 — GET /pushrules/ returns 200 with full ruleset grouped by kind under {"global":{...}}
  # AC2 — Default rules are seeded lazily on first GET /pushrules/ for a user
  Scenario: GetPushrules_ReturnsDefaultRules — first call seeds and returns default rules
    When kai calls GET /_matrix/client/v3/pushrules/
    Then the response status is 200
    And the response body contains "global"
    And the pushrules response has a global.override array containing rule_id "m.rule.master"

  # AC2 — Lazy seeding is idempotent: second call produces no duplicate rows
  Scenario: GetPushrules_LazySeeding_Idempotent — second call returns same rule count without duplicates
    When kai calls GET /_matrix/client/v3/pushrules/
    And kai calls GET /_matrix/client/v3/pushrules/ again
    Then the response status is 200
    And the default push rule count is the same as after the first call

  # AC3 — GET single rule returns 200 with the rule object
  Scenario: GetPushrulesRule_ExistingRule_Returns200 — GET single rule returns rule object
    Given kai has called GET /_matrix/client/v3/pushrules/ to seed default rules
    When kai calls GET /_matrix/client/v3/pushrules/global/override/m.rule.master
    Then the response status is 200
    And the response body contains "rule_id"
    And the response body contains "m.rule.master"

  # AC3 — GET non-existent rule returns 404 M_NOT_FOUND
  Scenario: GetPushrulesRule_NonExistent_Returns404 — GET unknown rule returns M_NOT_FOUND
    When kai calls GET /_matrix/client/v3/pushrules/global/override/nonexistent.rule
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC4 — PUT creates a custom rule; subsequent GET returns it
  Scenario: PutPushrule_CreatesCustomRule — PUT custom rule and GET returns it
    When kai calls PUT /_matrix/client/v3/pushrules/global/override/my.rule.test with body {"conditions":[],"actions":["notify"]}
    Then the response status is 200
    When kai calls GET /_matrix/client/v3/pushrules/global/override/my.rule.test
    Then the response status is 200
    And the response body contains "my.rule.test"

  # AC4 — PUT to overwrite a default rule returns 400 M_INVALID_PARAM
  Scenario: PutPushrule_DefaultRule_Rejected — PUT to default rule returns M_INVALID_PARAM
    Given kai has called GET /_matrix/client/v3/pushrules/ to seed default rules
    When kai calls PUT /_matrix/client/v3/pushrules/global/override/m.rule.master with body {"conditions":[],"actions":["notify"]}
    Then the response status is 400
    And the response body contains "M_INVALID_PARAM"

  # AC5 — DELETE custom rule returns 200; subsequent GET returns 404
  Scenario: DeletePushrule_CustomRule_Succeeds — DELETE custom rule removes it
    Given kai has a custom push rule "my.rule.test" in kind "override"
    When kai calls DELETE /_matrix/client/v3/pushrules/global/override/my.rule.test
    Then the response status is 200
    When kai calls GET /_matrix/client/v3/pushrules/global/override/my.rule.test
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC5 — DELETE default rule returns 400 M_INVALID_PARAM
  Scenario: DeletePushrule_DefaultRule_Rejected — DELETE default rule returns M_INVALID_PARAM
    Given kai has called GET /_matrix/client/v3/pushrules/ to seed default rules
    When kai calls DELETE /_matrix/client/v3/pushrules/global/override/m.rule.master
    Then the response status is 400
    And the response body contains "M_INVALID_PARAM"

  # AC6 — PUT /{ruleId}/enabled enables or disables any rule including defaults
  Scenario: PutPushruleEnabled_ToggleDefaultRule — PUT /enabled disables m.rule.master
    Given kai has called GET /_matrix/client/v3/pushrules/ to seed default rules
    When kai calls PUT /_matrix/client/v3/pushrules/global/override/m.rule.master/enabled with body {"enabled":false}
    Then the response status is 200
    When kai calls GET /_matrix/client/v3/pushrules/global/override/m.rule.master
    Then the response status is 200
    And the push rule enabled field is false

  # AC7 — PUT /{ruleId}/actions replaces actions of any rule including defaults
  Scenario: PutPushruleActions_UpdatesActions — PUT /actions replaces the actions array
    Given kai has a custom push rule "my.rule.test" in kind "override"
    When kai calls PUT /_matrix/client/v3/pushrules/global/override/my.rule.test/actions with body {"actions":["dont_notify"]}
    Then the response status is 200
    When kai calls GET /_matrix/client/v3/pushrules/global/override/my.rule.test
    Then the response status is 200
    And the push rule actions contain "dont_notify"

  # AC8 — Non-"global" scope returns 400 M_INVALID_PARAM
  Scenario: InvalidScope_Returns400 — scope "device" is not supported
    When kai calls GET /_matrix/client/v3/pushrules/device/override/m.rule.master
    Then the response status is 400
    And the response body contains "M_INVALID_PARAM"

  # AC9 — GET /pushers returns 200 with {"pushers":[]} when none registered
  Scenario: GetPushers_EmptyList — no pushers registered returns empty array
    When kai calls GET /_matrix/client/v3/pushers
    Then the response status is 200
    And the response body contains "pushers"
    And the pushers array is empty

  # AC10 — POST /pushers/set registers a pusher; POST with kind=null deregisters it
  Scenario: PostPushersSet_RegisterAndDeregister — register then deregister a pusher
    When kai calls POST /_matrix/client/v3/pushers/set with body {"pushkey":"pk1","kind":"http","app_id":"app1","app_display_name":"Test","device_display_name":"Phone","lang":"en","data":{"url":"https://example.com/push"}}
    Then the response status is 200
    When kai calls GET /_matrix/client/v3/pushers
    Then the response status is 200
    And the pushers array has 1 item
    When kai calls POST /_matrix/client/v3/pushers/set with body {"pushkey":"pk1","kind":null,"app_id":"app1"}
    Then the response status is 200
    When kai calls GET /_matrix/client/v3/pushers
    Then the response status is 200
    And the pushers array is empty
