Feature: Device Management — GET/PUT/DELETE /devices + POST /delete_devices
  As an end-user
  I want to list, rename, and delete my active login sessions (devices) from within any Matrix client
  So that I can maintain control over which devices have access to my account

  # Story 7-26 — ATDD: tests written FIRST (red phase), before implementation.
  # All scenarios below MUST FAIL until the Device Management handlers are implemented
  # (routes not yet registered in main.go → 404 for all requests except GET /devices stub).

  Background:
    Given the docker compose stack is started
    And kai is authenticated via OIDC

  # AC1 — GET /devices returns the real session list for the authenticated user
  Scenario: ListDevices_AuthenticatedUser_ReturnsDevicesArray — GET /devices returns 200 with devices array
    When kai calls GET /_matrix/client/v3/devices
    Then the response status is 200
    And the response body contains "devices"

  # AC2 — GET /devices/{deviceId} returns a single device object
  Scenario: GetDevice_KnownDeviceId_Returns200 — GET /devices/{deviceId} returns the device
    Given kai has a known device ID
    When kai calls GET /_matrix/client/v3/devices/{deviceId}
    Then the response status is 200
    And the response body contains "device_id"

  # AC2 — GET /devices/{deviceId} returns 404 for unknown deviceId
  Scenario: GetDevice_UnknownDeviceId_Returns404 — GET /devices/{unknown} returns M_NOT_FOUND
    When kai calls GET /_matrix/client/v3/devices/UNKNOWN_DEVICE_XYZ_999
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC3 — PUT /devices/{deviceId} updates display_name
  Scenario: UpdateDevice_ValidDisplayName_Returns200 — PUT updates display_name and GET reflects it
    Given kai has a known device ID
    When kai calls PUT /_matrix/client/v3/devices/{deviceId} with body {"display_name":"Work Laptop"}
    Then the response status is 200
    And the response body is "{}"
    When kai calls GET /_matrix/client/v3/devices/{deviceId}
    Then the response status is 200
    And the response body contains "Work Laptop"

  # AC3 — PUT /devices/{deviceId} returns 404 for unknown deviceId
  Scenario: UpdateDevice_UnknownDeviceId_Returns404 — PUT on unknown device returns M_NOT_FOUND
    When kai calls PUT /_matrix/client/v3/devices/UNKNOWN_DEVICE_XYZ_999 with body {"display_name":"Test"}
    Then the response status is 404
    And the response body contains "M_NOT_FOUND"

  # AC4 — DELETE /devices/{deviceId} requires UIA: no auth body → 401 challenge
  Scenario: DeleteDevice_NoAuthBody_Returns401Challenge — DELETE without auth returns UIA challenge
    Given kai has a known device ID
    When kai calls DELETE /_matrix/client/v3/devices/{deviceId} with no body
    Then the response status is 401
    And the response body contains "flows"
    And the response body contains "session"
    And the response body contains "params"

  # AC5 — DELETE own current device is forbidden
  Scenario: DeleteDevice_OwnCurrentDevice_Returns403 — cannot delete the device you are currently using
    Given kai has a known device ID
    When kai calls DELETE /_matrix/client/v3/devices/{deviceId} with completed UIA
    Then the response status is 403
    And the response body contains "M_FORBIDDEN"

  # JWT required — unauthenticated request is rejected
  Scenario: ListDevices_Unauthenticated_Returns401 — request without JWT is rejected
    When an unauthenticated client calls GET /_matrix/client/v3/devices
    Then the response status is 401
