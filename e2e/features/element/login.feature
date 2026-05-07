Feature: Element Web Login
  As a Nebu user
  I want to log in to Element Web via Dex OIDC
  So that I can access my rooms and messages

  # RED PHASE: All scenarios fail because:
  # 1. playwright-bdd is not installed (AC1)
  # 2. ensureStorageState() is not implemented (AC2/AC3)
  # 3. Step definitions in login.steps.ts are stubs (AC6)

  Background:
    Given the Nebu stack is running

  @ac6-login
  Scenario: SSO Login via Dex shows room list
    Given alex has no cached session
    When alex opens Element Web and clicks "Sign in"
    And alex authenticates via Dex with "alex@example.com"
    Then the room list is visible
    And no error dialog appears

  @ac6-logout
  Scenario: Logout redirects to welcome screen
    Given alex is logged in via Element Web
    When alex opens the user menu and clicks "Sign out"
    Then the welcome screen is visible
    And the "Sign in" button is present

  @ac6-relogin
  Scenario: Cached session — no OIDC redirect on reload
    Given alex is logged in via Element Web
    When alex reloads Element Web
    Then the room list is visible without a Dex redirect
