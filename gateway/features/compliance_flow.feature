Feature: Compliance Flow — Four-Eyes Export, GDPR Deletion, Audit Immutability
  As a developer
  I want to verify the full compliance workflow end-to-end
  So that CI catches regressions in the audit, four-eyes approval, and export paths

  Background:
    Given the docker compose stack is started

  Scenario: Full Four-Eyes Compliance Export
    Given two compliance officers are authenticated with valid Matrix sessions
    And officer_a creates a room with at least one message
    When officer_a submits a compliance access request for the room
    Then the response status is 201
    And the response body contains "pending"
    When officer_a tries to approve their own access request
    Then the response status is 403
    And the response body contains "Self-approval"
    When officer_b approves the compliance access request
    Then the response status is 200
    And the response body contains "approved"
    When officer_a creates a compliance session for the approved request
    Then the response status is 201
    And the response body contains "session_token"
    When officer_a calls GET /api/v1/compliance/export with the compliance session token
    Then the response status is 200
    And the response body contains "events"
    And the response body contains "server_signature"
    And the server_signature is verifiable with the server Ed25519 public key

  Scenario: GDPR Deletion and Anonymization
    Given an admin is authenticated and a victim user exists with displayname "Alice"
    When admin deletes keys for the victim user with reason "GDPR-2024-001"
    Then the response status is 200
    And the response body contains "keys_deleted"
    And the audit_log contains a row with action "user_keys_deleted" and outcome "success"
    When admin anonymizes the victim user
    Then the response status is 200
    And the Matrix profile for the victim user shows displayname "Deleted User"

  Scenario: Audit log immutability via RLS
    Given the audit_log table has at least one row
    When a direct SQL DELETE FROM audit_log is attempted using the application DB role
    Then PostgreSQL raises a policy violation error
