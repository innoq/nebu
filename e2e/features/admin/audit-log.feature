Feature: Admin UI — Audit Log
  As an authenticated admin operator
  I want to view the audit log in the Admin UI
  So that I can trace administrative actions

  # Story 9-26 — Phase 3, AC12.
  # New feature derived from admin spec files:
  # tests/features/admin/audit-log.spec.ts

  Background:
    Given the Nebu stack is running
    And bootstrap has been completed
    And the operator is logged in as admin

  @ac12-audit-log
  Scenario: Audit log page shows recent events
    When the operator navigates to "/admin/audit-log"
    Then the page shows "Audit"
    And the audit log contains at least one entry
