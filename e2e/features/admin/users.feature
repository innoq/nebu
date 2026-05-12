Feature: Admin UI — Users Page
  As an authenticated admin operator
  I want to view and manage users in the Admin UI
  So that I can see who has access to the Nebu instance

  # Story 9-26 — Phase 3, AC12.
  # New feature derived from admin spec files:
  # tests/features/admin/users-page.spec.ts, users-api-integration.spec.ts

  Background:
    Given the Nebu stack is running
    And bootstrap has been completed
    And the operator is logged in as admin

  @ac12-users-list
  Scenario: Users page shows the list of registered users
    When the operator navigates to "/admin/users"
    Then the page shows "Users"
    And the user list contains at least one entry
