defmodule Nebu.NodeRegistrationTest do
  use ExUnit.Case, async: true

  # Tests for Nebu.NodeRegistration.
  #
  # register_with_gateway/1 depends on network access (gateway) and file system
  # (PSK file). We test the failure paths by controlling env vars and verifying
  # the function completes without raising (fire-and-forget contract).
  #
  # The retry / success paths are integration-tested via `make dev`.

  describe "register_with_gateway/1 — PSK file missing" do
    test "logs error and returns when NEBU_INTERNAL_SECRET_FILE is not set" do
      # Unset the env var for this test
      original = System.get_env("NEBU_INTERNAL_SECRET_FILE")

      try do
        System.delete_env("NEBU_INTERNAL_SECRET_FILE")

        # Should not raise — graceful failure with 0 retries
        result = Nebu.NodeRegistration.register_with_gateway(0)
        # Returns :ok (from Logger.error/1) — function must not crash
        assert result == :ok
      after
        if original, do: System.put_env("NEBU_INTERNAL_SECRET_FILE", original)
      end
    end
  end

  describe "register_with_gateway/1 — PSK file not readable" do
    test "logs error when PSK file path does not exist (0 retries)" do
      original = System.get_env("NEBU_INTERNAL_SECRET_FILE")

      try do
        System.put_env("NEBU_INTERNAL_SECRET_FILE", "/nonexistent/path/secret")

        result = Nebu.NodeRegistration.register_with_gateway(0)
        assert result == :ok
      after
        if original do
          System.put_env("NEBU_INTERNAL_SECRET_FILE", original)
        else
          System.delete_env("NEBU_INTERNAL_SECRET_FILE")
        end
      end
    end
  end

  describe "register_with_gateway/1 — gateway unreachable" do
    test "exhausts retries when gateway URL is unreachable (0 retries, no sleep)" do
      original_file = System.get_env("NEBU_INTERNAL_SECRET_FILE")
      original_url = System.get_env("NEBU_GATEWAY_INTERNAL_URL")

      # Write a temporary PSK file
      tmp_path = System.tmp_dir!() |> Path.join("nebu_test_psk_#{:rand.uniform(100_000)}")
      File.write!(tmp_path, "test-psk-value\n")

      try do
        System.put_env("NEBU_INTERNAL_SECRET_FILE", tmp_path)
        # Point at a port that will immediately refuse connection
        System.put_env("NEBU_GATEWAY_INTERNAL_URL", "http://127.0.0.1:19999")

        # 0 retries so no Process.sleep — completes immediately
        result = Nebu.NodeRegistration.register_with_gateway(0)
        assert result == :ok
      after
        File.rm(tmp_path)

        if original_file do
          System.put_env("NEBU_INTERNAL_SECRET_FILE", original_file)
        else
          System.delete_env("NEBU_INTERNAL_SECRET_FILE")
        end

        if original_url do
          System.put_env("NEBU_GATEWAY_INTERNAL_URL", original_url)
        else
          System.delete_env("NEBU_GATEWAY_INTERNAL_URL")
        end
      end
    end
  end
end
