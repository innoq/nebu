defmodule Nebu.Health.InfoTest do
  @moduledoc """
  Acceptance tests for the core GET /info endpoint (AC2 + AC5).

  These tests are RED-phase scaffolds — they must fail before implementation
  because neither Nebu.BuildInfo nor the GET /info route in Nebu.Health.Server
  exist yet.

  Test strategy:
  - Start an isolated health server on a random port to avoid conflicting with
    any already-running instance on port 4000.
  - Send a raw TCP request (HTTP/1.1 framing, matching what gen_tcp :http_bin
    packet mode parses) and assert the response body.
  """

  use ExUnit.Case, async: false

  # Pick a free port for the test server so these tests can run without a
  # pre-existing listener on port 4000.
  @test_port 14_000

  setup do
    # Override the port so the test server does not collide with a running instance on port 4000.
    Application.put_env(:event_dispatcher, :health_port, @test_port)

    # If the application-supervised Health.Server is running (which it is in unit tests),
    # terminate it via the supervisor so it does not get auto-restarted, then start a
    # fresh instance manually for this test's isolated port.
    case Process.whereis(Nebu.Health.Server) do
      nil ->
        :ok

      _pid ->
        # Terminate the supervised child so the supervisor does not race-restart it.
        :ok = Supervisor.terminate_child(Nebu.Event.Supervisor, Nebu.Health.Server)
    end

    {:ok, _pid} = Nebu.Health.Server.start_link([])

    on_exit(fn ->
      # Stop the manually started test server.
      case Process.whereis(Nebu.Health.Server) do
        nil -> :ok
        pid ->
          if Process.alive?(pid) do
            GenServer.stop(pid, :normal, 1_000)
          end
      end
      # Restart the supervised child so the rest of the test suite can use it.
      Supervisor.restart_child(Nebu.Event.Supervisor, Nebu.Health.Server)
      Application.delete_env(:event_dispatcher, :health_port)
    end)

    :ok
  end

  # ---------------------------------------------------------------------------
  # Helper: send a raw HTTP/1.1 GET request over TCP and return the full response.
  # ---------------------------------------------------------------------------
  defp http_get(path) do
    {:ok, socket} = :gen_tcp.connect(~c"127.0.0.1", @test_port, [:binary, packet: :raw, active: false])
    request = "GET #{path} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n"
    :ok = :gen_tcp.send(socket, request)

    response = recv_all(socket, "")
    :gen_tcp.close(socket)
    response
  end

  defp recv_all(socket, acc) do
    case :gen_tcp.recv(socket, 0, 3_000) do
      {:ok, data}      -> recv_all(socket, acc <> data)
      {:error, :closed} -> acc
      {:error, _}      -> acc
    end
  end

  # ---------------------------------------------------------------------------
  # AC2 + AC5 — GET /info returns 200 with build metadata JSON
  # ---------------------------------------------------------------------------

  test "GET /info returns 200 with build metadata when build_info is configured" do
    Application.put_env(:event_dispatcher, :build_info, %{
      version: "0.1.0",
      git_commit: "abc1234",
      build_time: "2026-05-11T10:00:00Z"
    })

    on_exit(fn -> Application.delete_env(:event_dispatcher, :build_info) end)

    response = http_get("/info")

    # Status line
    assert String.contains?(response, "HTTP/1.1 200 OK"),
      "Expected HTTP/1.1 200 OK in response, got: #{inspect(String.slice(response, 0, 80))}"

    # Content-Type header
    assert String.contains?(response, "Content-Type: application/json"),
      "Expected Content-Type: application/json in response headers"

    # Parse body (everything after the blank CRLF separator)
    [_headers, body] = String.split(response, "\r\n\r\n", parts: 2)
    assert {:ok, decoded} = Jason.decode(body)

    assert decoded["component"] == "core",
      "Expected component==\"core\", got: #{inspect(decoded["component"])}"

    assert decoded["version"] == "0.1.0",
      "Expected version==\"0.1.0\", got: #{inspect(decoded["version"])}"

    assert decoded["git_commit"] == "abc1234",
      "Expected git_commit==\"abc1234\", got: #{inspect(decoded["git_commit"])}"

    assert decoded["build_time"] == "2026-05-11T10:00:00Z",
      "Expected build_time==\"2026-05-11T10:00:00Z\", got: #{inspect(decoded["build_time"])}"

    # AC5: /info MUST reuse Nebu.Health.Server — no new named GenServer allowed
    refute Process.whereis(Nebu.InfoServer),
      "AC5 violation: a new named process Nebu.InfoServer was registered — /info must extend Nebu.Health.Server"
  end

  # ---------------------------------------------------------------------------
  # AC3 (core variant) — fallback to "unknown" when build_info is not configured
  # ---------------------------------------------------------------------------

  test "GET /info returns 200 with unknown fallbacks when build_info is not set" do
    Application.delete_env(:event_dispatcher, :build_info)

    response = http_get("/info")

    assert String.contains?(response, "HTTP/1.1 200 OK"),
      "Expected HTTP/1.1 200 OK in response, got: #{inspect(String.slice(response, 0, 80))}"

    [_headers, body] = String.split(response, "\r\n\r\n", parts: 2)
    assert {:ok, decoded} = Jason.decode(body)

    # All metadata fields must be present and non-empty
    for field <- ["component", "version", "git_commit", "build_time"] do
      assert Map.has_key?(decoded, field),
        "Expected field #{inspect(field)} in response body"
      refute decoded[field] == "",
        "Field #{inspect(field)} must not be an empty string — want \"unknown\""
    end

    assert decoded["component"] == "core",
      "Expected component==\"core\", got: #{inspect(decoded["component"])}"

    assert decoded["version"] == "unknown",
      "Expected version==\"unknown\", got: #{inspect(decoded["version"])}"

    assert decoded["git_commit"] == "unknown",
      "Expected git_commit==\"unknown\", got: #{inspect(decoded["git_commit"])}"

    assert decoded["build_time"] == "unknown",
      "Expected build_time==\"unknown\", got: #{inspect(decoded["build_time"])}"
  end
end
