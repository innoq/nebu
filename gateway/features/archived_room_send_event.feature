Feature: Send Event to Archived Room — 403 M_ROOM_ARCHIVED (Story 9-9 TOCTOU Fix)
  As a Matrix client
  I want send-event requests to archived rooms to be rejected with a clear error
  So that the archive invariant is enforced end-to-end, even when Core closes the TOCTOU race window

  # Story 9-9 — ATDD: tests written FIRST (red phase), before implementation.
  # Background: SEC Gate 2 HIGH-2 (epic-6). The TOCTOU race window between
  # archive_room_atomic/1 and Core's send_event handler is closed by adding a
  # SELECT FOR UPDATE check in Room.Server.handle_call({:send_event, ...}).
  # When Core rejects the event, it returns gRPC FAILED_PRECONDITION "M_ROOM_ARCHIVED".
  # The Gateway maps codes.FailedPrecondition → 403 M_ROOM_ARCHIVED.
  #
  # These scenarios must FAIL until:
  #   - Nebu.Room.DBBehaviour gains @callback check_room_status_for_update/1
  #   - Room.Server.handle_call({:send_event,...}) calls check_room_status_for_update/1
  #   - EventDispatcher.Server maps {:error, :room_archived} → GRPC.Status.failed_precondition()
  #   - Gateway PutSendEventHandler maps codes.FailedPrecondition → 403 M_ROOM_ARCHIVED
  #
  # Error format (same as existing gateway-level archive guard, Story 6.9):
  #   HTTP 403 {"errcode":"M_ROOM_ARCHIVED","error":"Room is archived"}

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC
    And alex is authenticated via OIDC

  # ─── AC2 + AC3: Core TOCTOU guard → 403 M_ROOM_ARCHIVED ──────────────────

  # AC2 + AC3 [P0]: When a room is archived and the Core SELECT FOR UPDATE check
  # fires, the end-to-end response must be 403 M_ROOM_ARCHIVED.
  Scenario: ArchivedRoom_SendEvent_Returns403_CoreTOCTOUPath
    Given kai creates a room named "toctou-test-room"
    And kai invites alex to the room
    And alex joins the room
    And an admin archives the room via the Admin API
    When alex sends the message "this must not be written" to the room
    Then the response status is 403
    And the response body contains "M_ROOM_ARCHIVED"

  # AC2 [P0]: The 403 response body must match exactly the same format produced
  # by the existing gateway-level archive guard (Story 6.9 AC#4).
  # Both guards produce identical 403 bodies — consistency test.
  Scenario: ArchivedRoom_SendEvent_ResponseBody_MatchesGatewayGuardFormat
    Given kai creates a room named "toctou-body-test-room"
    And an admin archives the room via the Admin API
    When kai sends the message "body format check" to the room
    Then the response status is 403
    And the response body has errcode "M_ROOM_ARCHIVED"
    And the response body has error message "Room is archived"

  # ─── AC4: Active room is unaffected by the new TOCTOU check ───────────────

  # AC4 [P0]: The new SELECT FOR UPDATE check must not regress the happy path.
  # Sending to an active room must still return 200 with event_id.
  Scenario: ActiveRoom_SendEvent_Succeeds_TOCTOUCheckUnaffected
    Given kai creates a room named "active-room-toctou"
    And kai invites alex to the room
    And alex joins the room
    When alex sends the message "hello from active room" to the room
    Then the response status is 200
    And the response body contains "event_id"

  # ─── AC4: Idempotency regression guard ────────────────────────────────────

  # AC4 [P1]: The txnId idempotency check must still work correctly after the
  # TOCTOU fix is added (the idempotency check happens BEFORE the archived check,
  # so a second send with the same txnId on an active room returns the same event_id).
  Scenario: ActiveRoom_SendEvent_TxnIdIdempotency_UnaffectedByTOCTOUFix
    Given kai creates a room named "idempotency-toctou"
    When kai sends the message "idempotent" to the room
    Then the response status is 200
    And kai sends the same message again with the same txnId
    Then both sends returned the same event_id

  # ─── AC4: Unauthenticated request still rejected ──────────────────────────

  # AC4 [P1]: JWT auth must still be enforced even on archived rooms.
  Scenario: ArchivedRoom_SendEvent_Unauthenticated_Returns401
    Given kai creates a room named "auth-guard-toctou"
    And an admin archives the room via the Admin API
    When an unauthenticated client sends a message to the room
    Then the response status is 401
