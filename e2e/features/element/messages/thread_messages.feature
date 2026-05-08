Feature: Thread Messages via Element Web UI
  As a Nebu user
  I want to reply to messages in threads via the Element Web client
  So that I can have threaded conversations without cluttering the main timeline

  # Regression guard — Story 9-30: these scenarios fail if the
  # Postgrex.JSONB fix in event_map_to_proto/1 (server.ex) is reverted.
  # Root cause was: JSONB struct → Jason.encode! → Protocol.UndefinedError → 500.

  Background:
    Given the Nebu stack is running
    And a room "thread-msg-template" exists and alex is a member
    And marie is a member of room "thread-msg-template"
    And alex is logged in via Element Web

  # ─────────────────────────────────────────────────────────────────────────────
  # AC1: Thread indicator appears on the original message after a reply is sent
  # ─────────────────────────────────────────────────────────────────────────────

  @ac1-thread-indicator
  Scenario: Thread reply indicator appears on root message after reply
    Given marie is logged in via Element Web in a second browser context
    When alex navigates to room "thread-msg-template"
    And alex sends "Let's discuss this in a thread"
    And marie navigates to room "thread-msg-template" in the second context
    And marie opens the thread panel for the message "Let's discuss this in a thread"
    And marie types "This is my thread reply" in the thread composer
    And marie sends the thread reply
    Then the message "Let's discuss this in a thread" shows a thread indicator in alex's timeline
    And the thread indicator shows at least 1 reply

  # ─────────────────────────────────────────────────────────────────────────────
  # AC2: Thread panel opens and shows the reply when the indicator is clicked
  # ─────────────────────────────────────────────────────────────────────────────

  @ac2-thread-panel
  Scenario: Clicking thread indicator opens thread panel with reply
    Given marie is logged in via Element Web in a second browser context
    When alex navigates to room "thread-msg-template"
    And alex sends "Root message for thread panel test"
    And marie navigates to room "thread-msg-template" in the second context
    And marie opens the thread panel for the message "Root message for thread panel test"
    And marie types "Reply in thread panel" in the thread composer
    And marie sends the thread reply
    When alex clicks the thread indicator on "Root message for thread panel test"
    Then the thread panel is visible
    And the thread panel contains the message "Reply in thread panel"

  # ─────────────────────────────────────────────────────────────────────────────
  # AC3: /relations endpoint returns 200 (no 500 from Postgrex.JSONB bug)
  # AC1/AC2 exercise the thread indicator/panel via /sync bundled aggregations
  # (Story 9-28) and may pass without calling /relations. AC3 is the explicit
  # regression guard: it captures the GET /relations network response directly.
  #
  # Note: each scenario uses a unique timestamp-suffixed room (room-setup.steps.ts)
  # — no cross-scenario pollution even though all three use "thread-msg-template".
  #
  # Capture step is registered BEFORE opening the thread panel, because that is
  # when Element Web fires GET /relations (to populate the panel) — not on send.
  # ─────────────────────────────────────────────────────────────────────────────

  @ac3-relations-200
  Scenario: /relations endpoint returns 200 for thread messages (no 500 regression)
    When alex navigates to room "thread-msg-template"
    And alex sends "Message to verify relations endpoint"
    And alex captures the next /relations response
    And alex opens the thread panel for the message "Message to verify relations endpoint"
    And alex types "A thread reply to trigger /relations" in the thread composer
    And alex sends the thread reply
    Then the /relations request returns 200
    And the thread panel contains the message "A thread reply to trigger /relations"
