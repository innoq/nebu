Feature: Admin UI — Bootstrap and Dashboard Happy Path
  As an operator
  I want to set up Nebu via the Bootstrap Wizard and access the Dashboard
  So that the instance is ready to serve users

  # ─────────────────────────────────────────────────────────────────────────────
  # Scenario 1: Full Bootstrap Wizard Click-Through
  # Tests the complete onboarding flow from fresh instance to authenticated dashboard.
  # Prerequisites: no bootstrap_completed in server_config, Dex running on port 5556.
  # ─────────────────────────────────────────────────────────────────────────────
  Scenario: Operator completes Bootstrap Wizard via real OIDC login
    Given the Nebu admin UI is accessible at the admin URL
    And no bootstrap has been completed yet

    # Step 1: Instance name
    When the operator navigates to "/admin/"
    Then the browser is redirected to "/admin/bootstrap"
    And the page shows "Bootstrap Setup"
    And the progress indicator shows step 1 as active

    When the operator fills in "Instance Name" with "test-nebu"
    And the operator clicks "Next"
    Then the progress indicator shows step 2 as active

    # Step 2: OIDC config + real OIDC login
    When the operator fills in "OIDC Issuer URL" with the Dex issuer URL
    And the operator fills in "OIDC Client ID" with "nebu-admin"
    And the operator fills in "OIDC Client Secret" with the configured client secret
    And the operator clicks "Connect with OIDC"

    # Real OIDC Authorization Code flow
    Then the browser is redirected to the Dex login page
    When the operator fills in the Dex email field with "admin@example.com"
    And the operator fills in the Dex password field with the admin password
    And the operator clicks the Dex login submit button
    Then the browser is redirected back to "/admin/bootstrap/select-claim"

    # Claim selection
    And the page shows the discovered OIDC claims
    And the page shows at least one claim value as a selectable option
    When the operator selects the claim value "instance_admin" as the admin group claim
    And the operator clicks "Confirm"

    # Post-bootstrap: directly to dashboard
    Then the browser is redirected to "/admin/dashboard"
    And the page shows "Dashboard"
    And the operator is authenticated (no redirect to login)

  # ─────────────────────────────────────────────────────────────────────────────
  # Scenario 2: Dashboard shows live metrics after instance setup
  # Prerequisites: bootstrap completed, admin session active, Epic 4 stories done
  # Note: asserts on metrics widget require Session Manager + Room GenServer (Epic 4)
  # ─────────────────────────────────────────────────────────────────────────────
  Scenario: Dashboard displays live SSE metrics after rooms and sessions exist
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed
    And the operator is logged in as admin

    When the operator navigates to "/admin/dashboard"
    Then the page shows "Dashboard"
    And the status card for "Gateway" shows status "green"
    And the status card for "Core" shows status "green"
    And the status card for "Database" shows status "green"

    # SSE widget: live metrics (requires Epic 4 Session Manager + Room GenServer)
    And the metrics widget shows a "msg_per_sec" value
    And the metrics widget shows an "active_sessions" value
    And the metrics widget shows a "room_count" value

  # ─────────────────────────────────────────────────────────────────────────────
  # Scenario 3: Unauthenticated access is redirected to login
  # ─────────────────────────────────────────────────────────────────────────────
  Scenario: Unauthenticated operator is redirected to login
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed

    When the operator navigates to "/admin/dashboard"
    Then the browser is redirected to "/admin/login"
    And the page shows "Sign in"

  # ─────────────────────────────────────────────────────────────────────────────
  # Scenario 4: Admin login via OIDC after bootstrap
  # ─────────────────────────────────────────────────────────────────────────────
  Scenario: Admin logs in via OIDC and reaches dashboard
    Given the Nebu admin UI is accessible at the admin URL
    And bootstrap has been completed

    When the operator navigates to "/admin/login"
    And the operator clicks "Sign in with SSO"
    Then the browser is redirected to the Dex login page
    When the operator fills in the Dex email field with "admin@example.com"
    And the operator fills in the Dex password field with the admin password
    And the operator clicks the Dex login submit button
    Then the browser is redirected to "/admin/dashboard"
    And the page shows "Dashboard"
