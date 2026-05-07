Feature: Room Leave via Element Web UI
  As a Nebu user who is a member of a room
  I want to leave the room via the Element Web UI
  So that I am no longer a participant

  # RED PHASE: Fails because:
  # 1. playwright-bdd not installed
  # 2. getApiSession / createRoom / inviteUser are stubs
  # 3. Room menu "Leave room" click not implemented in room.steps.ts

  Background:
    Given the Nebu stack is running
    And a room "leave-test-template" exists and alex is a member
    And alex is logged in via Element Web

  @ac9-leave-room
  Scenario: Leave a room via UI
    When alex navigates to room "leave-test-template"
    And alex opens the room menu and clicks "Leave room"
    And alex confirms leaving
    Then the room "leave-test-template" is not in alex's sidebar
