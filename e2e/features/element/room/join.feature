Feature: Room Join via Invite in Element Web UI
  As a Nebu user who has been invited to a room
  I want to accept the invite via the Element Web UI
  So that I can participate in the room

  # RED PHASE: Fails because:
  # 1. playwright-bdd not installed
  # 2. getApiSession / createRoom / inviteUser are stubs (dex-auth.ts)
  # 3. Invite accept UI step not implemented in room.steps.ts

  Background:
    Given the Nebu stack is running
    And a room "invite-test-template" exists and kai is the owner

  @ac8-join-room
  Scenario: Accept invite via UI
    Given kai has invited alex to "invite-test-template"
    And alex is logged in via Element Web
    When the invite for "invite-test-template" appears in alex's sidebar
    And alex clicks "Accept"
    Then the room "invite-test-template" appears in the sidebar
