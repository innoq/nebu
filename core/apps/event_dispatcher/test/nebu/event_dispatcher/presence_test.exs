defmodule Nebu.EventDispatcher.PresenceTest do
  use ExUnit.Case, async: false

  # ─── Story 4-18: Elixir get_presence/2 gRPC handler ──────────────────────────
  #
  # These tests are written FIRST (ATDD gate), before implementation exists.
  # ALL tests in this module are expected to FAIL until Story 4-18 is implemented.
  #
  # async: false — Nebu.Presence.Manager uses the :NebuPresence ETS table owned by
  # Nebu.Presence.Application. It is a process-global resource. Tests must not
  # run concurrently to avoid cross-test ETS pollution.
  #
  # Test strategy:
  #   - All tests call Nebu.EventDispatcher.Server.get_presence/2 directly
  #     (synchronous unary handler — no spawning needed).
  #   - Fake stream: minimal map with http_request_headers (matches
  #     Nebu.Grpc.Metadata.trusted_identity/1 contract used by other handlers).
  #   - No configurable DB module: get_presence/2 reads directly from
  #     Nebu.Presence.Manager.get_presence/1 (which reads from :NebuPresence ETS).
  #   - ETS :NebuPresence is owned by Nebu.Presence.Application and must be present
  #     in the test environment. Tests insert presence entries directly into the
  #     :NebuPresence ETS table to control state without calling the GenServer cast
  #     (avoids async race conditions).
  #   - Known user test: insert a :NebuPresence entry for @alice before calling
  #     get_presence; assert response has presence "online" and last_active_ago > 0.
  #   - Unknown user test: ensure @unknown:test.local has NO entry in :NebuPresence;
  #     get_presence must return {presence: "offline", last_active_ago: 0} WITHOUT
  #     raising any error (IMPORTANT MVP DECISION — no 404 for unknown users).

  alias Nebu.EventDispatcher.Server

  # ─── Fake stream ─────────────────────────────────────────────────────────────
  #
  # Minimal fake stream satisfying Nebu.Grpc.Metadata.trusted_identity/1.
  # get_presence/2 does not use trusted_identity (no ownership check — reading
  # any user's presence is allowed), but the handler signature requires a stream.

  defp build_stream(user_id, system_role \\ "user") do
    %{
      http_request_headers: %{
        "x-user-id" => user_id,
        "x-system-role" => system_role
      }
    }
  end

  # ─── Setup / Teardown ────────────────────────────────────────────────────────

  setup do
    # Override server_name for deterministic assertions.
    Application.put_env(:event_dispatcher, :server_name, "test.local")

    # Initialise :pg (built-in OTP, idempotent).
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    on_exit(fn ->
      Application.delete_env(:event_dispatcher, :server_name)

      # Remove any presence entries inserted by this test to prevent leakage.
      # :NebuPresence ETS table is owned by Nebu.Presence.Application —
      # do NOT delete the table, only clean up the specific test keys.
      for user_id <- ["@alice:test.local", "@unknown:test.local"] do
        if :ets.info(:NebuPresence) != :undefined do
          :ets.delete(:NebuPresence, user_id)
        end
      end
    end)

    :ok
  end

  # ─── Test 1: Known user with :online presence → GetPresenceResponse ──────────
  #
  # Given: :NebuPresence ETS has "@alice:test.local" with status :online and
  #        last_active_at = T (where T is System.system_time(:millisecond) - 5000)
  # When: Server.get_presence/2 called with {user_id: "@alice:test.local"}
  # Then: returns %Core.GetPresenceResponse{presence: "online", last_active_ago: ~5000}
  #       (within a tolerance of ±200ms for test timing variance)

  describe "Server.get_presence/2 — known user" do
    test "returns online status and computed last_active_ago for known user" do
      alice = "@alice:test.local"
      now_ms = System.system_time(:millisecond)
      last_active_at = now_ms - 5000

      # Insert presence entry directly into ETS to control state deterministically.
      # :NebuPresence must exist — it is owned by Nebu.Presence.Application.
      :ets.insert(:NebuPresence, {alice, :online, last_active_at})

      request = %Core.GetPresenceRequest{user_id: alice}

      response = Server.get_presence(request, build_stream(alice))

      assert %Core.GetPresenceResponse{} = response,
             "expected %Core.GetPresenceResponse{}, got: #{inspect(response)}"

      assert response.presence == "online",
             "expected presence=online, got: #{inspect(response.presence)}"

      # last_active_ago should be approximately 5000ms; allow ±200ms for timing.
      assert response.last_active_ago >= 4800 and response.last_active_ago <= 5200,
             "expected last_active_ago ~5000ms, got: #{response.last_active_ago}"
    end
  end

  # ─── Test 2: Unknown user → offline default, no error ────────────────────────
  #
  # Given: "@unknown:test.local" has NO entry in :NebuPresence ETS
  # When: Server.get_presence/2 called with {user_id: "@unknown:test.local"}
  # Then: returns %Core.GetPresenceResponse{presence: "offline", last_active_ago: 0}
  #       without raising any exception (IMPORTANT MVP DECISION — no 404 for unknown users)

  describe "Server.get_presence/2 — unknown user" do
    test "returns offline with last_active_ago=0 for user never seen (no error raised)" do
      unknown = "@unknown:test.local"

      # Ensure no stale entry from a previous test run.
      if :ets.info(:NebuPresence) != :undefined do
        :ets.delete(:NebuPresence, unknown)
      end

      request = %Core.GetPresenceRequest{user_id: unknown}

      # Must NOT raise — Nebu.Presence.Manager.get_presence/1 defaults to offline.
      response =
        try do
          Server.get_presence(request, build_stream(unknown))
        rescue
          e ->
            flunk("get_presence must not raise for unknown user, got: #{inspect(e)}")
        end

      assert %Core.GetPresenceResponse{} = response,
             "expected %Core.GetPresenceResponse{}, got: #{inspect(response)}"

      assert response.presence == "offline",
             "expected presence=offline for unknown user, got: #{inspect(response.presence)}"

      assert response.last_active_ago == 0,
             "expected last_active_ago=0 for user never seen, got: #{response.last_active_ago}"
    end
  end
end
