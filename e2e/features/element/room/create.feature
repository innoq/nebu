Feature: Room Creation via Element Web UI
  As a logged-in Nebu user
  I want to create a new room via the Element Web UI
  So that I can start conversations with colleagues

  # RED PHASE: Fails because:
  # 1. playwright-bdd not installed
  # 2. openCreateRoomDialog() not implemented in ElementAppPage
  # 3. Room name timestamp substitution not yet handled in steps

  Background:
    Given the Nebu stack is running
    And alex is logged in via Element Web

  @ac7-create-room
  Scenario: Create a new room via UI
    When alex opens the "New room" dialog
    And alex enters room name "e2e-create-template"
    And alex clicks "Create room"
    Then the room "e2e-create-template" appears in the sidebar
    And the room header shows "e2e-create-template"
