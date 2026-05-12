Feature: Receive Message via Element Web UI
  As a Nebu user in a shared room
  I want to see messages sent by another user appear in my timeline
  So that real-time communication is verified end-to-end

  # RED PHASE: Fails because:
  # 1. playwright-bdd not installed
  # 2. createRoom + inviteUser stubs throw
  # 3. Second browser context (marie) not implemented yet
  # 4. .mx_EventTile with alex's message not visible in marie's timeline

  Background:
    Given the Nebu stack is running
    And a room "msg-recv-template" exists and alex is a member
    And marie is a member of room "msg-recv-template"

  @ac11-receive-message
  Scenario: User A sends, User B receives in timeline
    Given alex is logged in via Element Web
    And marie is logged in via Element Web in a second browser context
    When alex navigates to room "msg-recv-template"
    And alex sends "message from alex template"
    Then marie sees "message from alex template" in her timeline for "msg-recv-template"
