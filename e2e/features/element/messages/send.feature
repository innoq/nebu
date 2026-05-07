Feature: Send Message via Element Web UI
  As a logged-in Nebu user
  I want to type and send a message in a room via the Element Web composer
  So that other members can see my message in the timeline

  # RED PHASE: Fails because:
  # 1. playwright-bdd not installed
  # 2. createRoom + inviteUser stubs throw
  # 3. getComposerField() doesn't find the element (room not open)
  # 4. .mx_EventTile assertion fails (no message was sent)

  Background:
    Given the Nebu stack is running
    And a room "msg-send-template" exists and alex is a member
    And alex is logged in via Element Web

  @ac10-send-message
  Scenario: Sent message appears in timeline
    When alex navigates to room "msg-send-template"
    And alex types "hello e2e template" in the composer
    And alex presses Enter
    Then the message "hello e2e template" is visible in the timeline
    And the message shows no error status
