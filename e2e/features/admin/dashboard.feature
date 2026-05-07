Feature: Admin UI — Dashboard
  As an authenticated operator
  I want to view the Dashboard with live metrics
  So that I can monitor the health of the Nebu instance

  # Story 9-26 — Phase 3, AC12.
  # Extracted from e2e/features/admin_ui.feature Scenario 2.

  Background:
    Given the Nebu stack is running

  @ac12-dashboard-metrics
  Scenario: Dashboard displays live SSE metrics after rooms and sessions exist
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed
    And the operator is logged in as admin
    When the operator navigates to "/admin/dashboard"
    Then the page shows "Dashboard"
    And the status card for "Gateway" shows status "green"
    And the status card for "Core" shows status "green"
    And the status card for "Database" shows status "green"
    And the metrics widget shows a "msg_per_sec" value
    And the metrics widget shows an "active_sessions" value
    And the metrics widget shows a "room_count" value
