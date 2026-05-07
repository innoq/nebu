Feature: Admin UI — Rooms Page
  As an authenticated admin operator
  I want to view the list of rooms in the Admin UI
  So that I can monitor room usage

  # Story 9-26 — Phase 3, AC12.
  # New feature derived from admin spec files:
  # tests/features/admin/rooms-page.spec.ts, rooms-api-integration.spec.ts

  Background:
    Given the Nebu stack is running
    And bootstrap has been completed
    And the operator is logged in as admin

  @ac12-rooms-list
  Scenario: Rooms page shows the list of existing rooms
    When the operator navigates to "/admin/rooms"
    Then the page shows "Rooms"
    And the room list contains at least one entry
