defmodule Nebu.Grpc.AuthInterceptor do
  @moduledoc """
  gRPC server interceptor that enforces node-registration token authentication.

  Every incoming RPC must carry the PSK in the `x-nebu-node-token` HTTP/2 header.
  Requests without a valid token are rejected with `GRPC.Status.unauthenticated()`.

  Story 5.29a — Block B (FB-52-01): authenticated gRPC channel between gateway and core.

  ## Token verification
  - Token is read from `x-nebu-node-token` request metadata.
  - Verified against the secret file path in `NEBU_INTERNAL_SECRET_FILE`.
  - Comparison is constant-time to prevent timing attacks.

  ## Configuration
  - `NEBU_INTERNAL_SECRET_FILE` env var: path to the shared-secret file.
    If not set, all requests are rejected (fail-secure default).
  - Test override: `Application.put_env(:event_dispatcher, :internal_secret, "test-secret")`.
    This allows unit tests to inject a known secret without a real file on disk.
  """

  @behaviour GRPC.Server.Interceptor

  require Logger

  @token_header "x-nebu-node-token"

  @impl GRPC.Server.Interceptor
  def init(opts), do: opts

  @impl GRPC.Server.Interceptor
  def call(req, stream, next, _opts) do
    token = get_token(stream)

    case verify_token(token) do
      :ok ->
        next.(req, stream)

      {:error, :missing} ->
        raise GRPC.RPCError,
          status: GRPC.Status.unauthenticated(),
          message: "missing x-nebu-node-token"

      {:error, :invalid} ->
        raise GRPC.RPCError,
          status: GRPC.Status.unauthenticated(),
          message: "invalid x-nebu-node-token"
    end
  end

  # ─── Private ─────────────────────────────────────────────────────────────────

  defp get_token(stream) do
    Map.get(stream.http_request_headers, @token_header)
  end

  defp verify_token(nil), do: {:error, :missing}
  defp verify_token(""), do: {:error, :missing}

  defp verify_token(token) do
    expected = read_internal_secret()

    if expected == "" do
      # No secret configured → fail-secure (reject all requests).
      Logger.warning("Nebu.Grpc.AuthInterceptor: no internal secret configured; rejecting all gRPC calls")
      {:error, :invalid}
    else
      # Constant-time comparison prevents timing attacks.
      if secure_compare(token, expected) do
        :ok
      else
        {:error, :invalid}
      end
    end
  end

  # Read the shared secret. Checks Application env first (for tests),
  # then reads the file specified by NEBU_INTERNAL_SECRET_FILE.
  defp read_internal_secret do
    case Application.get_env(:event_dispatcher, :internal_secret) do
      nil ->
        case System.get_env("NEBU_INTERNAL_SECRET_FILE") do
          nil ->
            ""

          path ->
            case File.read(path) do
              {:ok, content} -> String.trim(content)
              {:error, reason} ->
                Logger.error("Nebu.Grpc.AuthInterceptor: cannot read secret file",
                  path: path,
                  reason: inspect(reason)
                )
                ""
            end
        end

      secret when is_binary(secret) ->
        String.trim(secret)
    end
  end

  # Constant-time binary comparison to prevent timing attacks.
  # Both binaries are padded to the same length before comparison.
  # Returns true only when both length and content are identical.
  defp secure_compare(a, b) when is_binary(a) and is_binary(b) do
    # Use :crypto.hash for constant-time comparison via HMAC equality.
    # Comparing HMAC(random_key, a) == HMAC(random_key, b) is safe and
    # constant-time because the random key is unknown to an attacker.
    key = :crypto.strong_rand_bytes(32)
    :crypto.mac(:hmac, :sha256, key, a) == :crypto.mac(:hmac, :sha256, key, b)
  end

  defp secure_compare(_, _), do: false
end
