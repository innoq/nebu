defmodule Nebu.NodeRegistration do
  @moduledoc """
  Registers this Elixir core node with the Go gateway on startup.

  Uses :httpc (built-in :inets) — no additional Hex dependencies required.
  Retries up to 5 times with 2-second backoff on connection errors.
  Registration failure never crashes the application (fire-and-forget via Task.start/1).
  """

  require Logger

  @max_retries 5
  @retry_delay_ms 2_000

  @doc """
  Attempts to register with the gateway. Called from the Application start/2 hook
  inside a Task.start/1 so it does not block or crash the supervisor tree.
  """
  def register_with_gateway(retries_left \\ @max_retries) do
    gateway_url = System.get_env("NEBU_GATEWAY_INTERNAL_URL", "http://gateway:8008")
    secret_file = System.get_env("NEBU_INTERNAL_SECRET_FILE")

    case read_psk(secret_file) do
      {:ok, psk} ->
        do_register_with_retries(gateway_url, String.trim(psk), retries_left)

      {:error, reason} ->
        Logger.error("Gateway registration failed: #{reason}")
    end
  end

  defp do_register_with_retries(gateway_url, psk, retries_left) do
    case do_register(gateway_url, psk) do
      :ok ->
        Logger.info("Registered with gateway")

      {:error, reason} when retries_left > 0 ->
        Logger.warning("Gateway registration failed: #{reason}, retrying (#{retries_left} left)")
        Process.sleep(@retry_delay_ms)
        do_register_with_retries(gateway_url, psk, retries_left - 1)

      {:error, reason} ->
        Logger.error("Gateway registration failed after retries: #{reason}")
    end
  end

  defp read_psk(nil), do: {:error, "NEBU_INTERNAL_SECRET_FILE not set"}

  defp read_psk(path) do
    case File.read(path) do
      {:ok, content} -> {:ok, content}
      {:error, reason} -> {:error, "cannot read PSK file: #{reason}"}
    end
  end

  defp do_register(gateway_url, psk) do
    url = String.to_charlist("#{gateway_url}/internal/nodes/register")
    headers = [{~c"authorization", String.to_charlist("Bearer #{psk}")}]
    request = {url, headers, ~c"application/json", ~c"{}"}

    case :httpc.request(:post, request, [{:timeout, 5_000}], []) do
      {:ok, {{_, 200, _}, _, _}} -> :ok
      {:ok, {{_, status, _}, _, _}} -> {:error, "unexpected status: #{status}"}
      {:error, reason} -> {:error, inspect(reason)}
    end
  end
end
