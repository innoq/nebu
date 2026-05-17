Feature: GDPR Right to Erasure — Story 14.4

  As a compliance officer
  I want to verify that deleting a user in Nebu correctly erases all PII and key material end-to-end
  So that GDPR Article 17 (Right to Erasure) can be attested with evidence

  Background:
    Given the docker compose stack is started

  Scenario: Full GDPR erasure verifies PII cleared keys nulled and sessions deleted
    Given an admin is authenticated for GDPR deletion tests
    And a victim user "gdpr_alice" exists with profile "Alice GDPR Test" for erasure
    And the victim user "gdpr_alice" has an active Matrix session
    And the victim user "gdpr_alice" has sent a message in a room
    When admin calls DELETE /api/v1/admin/users on the victim user
    Then the response status is 200
    And the response body contains "gdpr_deleted"
    And the profiles table shows displayname "Deleted User" for the victim user
    And the profiles table shows avatar_url NULL for the victim user
    And the user_keys table shows private_key NULL for both signing and encryption keys for the victim user
    And the users table shows anonymized_at is set for the victim user
    And the sessions table shows no active sessions for the victim user
    And the audit_log table contains a gdpr_deletion event for the victim user
    And the events table still contains the victim user message (room history preserved)

  Scenario: Matrix profile endpoint returns anonymized data after GDPR deletion
    Given an admin is authenticated for GDPR deletion tests
    And a victim user "gdpr_bob" exists with profile "Bob GDPR Test" for erasure
    And admin calls DELETE /api/v1/admin/users on the victim user
    And the response status is 200
    When GET /_matrix/client/v3/profile is called for the deleted victim user
    Then the response status is 200
    And the response body contains "Deleted User"
    And the response body does not contain "avatar_url"

  Scenario: OIDC login is blocked with M_USER_DEACTIVATED after GDPR deletion
    Given an admin is authenticated for GDPR deletion tests
    And a victim user "gdpr_carol" exists with profile "Carol GDPR Test" for erasure
    And admin calls DELETE /api/v1/admin/users on the victim user
    And the response status is 200
    When the deleted victim user attempts Matrix OIDC login
    Then the response status is 403
    And the response body has errcode "M_USER_DEACTIVATED"
