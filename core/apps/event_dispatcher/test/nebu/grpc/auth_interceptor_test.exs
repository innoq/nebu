defmodule Nebu.Grpc.AuthInterceptorTest do
  # async: false because we mutate Application env (:internal_secret).
  use ExUnit.Case, async: false

  alias Nebu.Grpc.AuthInterceptor

  @secret "the-correct-secret"

  setup do
    # Inject a known secret into Application env so the interceptor doesn't
    # try to read NEBU_INTERNAL_SECRET_FILE during tests.
    Application.put_env(:event_dispatcher, :internal_secret, @secret)

    on_exit(fn ->
      Application.delete_env(:event_dispatcher, :internal_secret)
    end)

    :ok
  end

  defp build_stream(headers) do
    %{http_request_headers: Map.new(headers)}
  end

  defp call_with_token(token_value) do
    headers =
      case token_value do
        :missing -> []
        v -> [{"x-nebu-node-token", v}]
      end

    stream = build_stream(headers)
    next = fn req, _stream -> {:next_called, req} end
    AuthInterceptor.call(:fake_req, stream, next, AuthInterceptor.init([]))
  end

  describe "call/4 — token verification" do
    # MINOR-2 coverage: nil token (header missing) → unauthenticated.
    test "rejects missing token (no x-nebu-node-token header)" do
      err =
        assert_raise GRPC.RPCError, fn ->
          call_with_token(:missing)
        end

      assert err.status == GRPC.Status.unauthenticated()
      assert err.message =~ "missing"
    end

    # MINOR-2 coverage: empty string token → treated as missing.
    test "rejects empty-string token" do
      err =
        assert_raise GRPC.RPCError, fn ->
          call_with_token("")
        end

      assert err.status == GRPC.Status.unauthenticated()
      assert err.message =~ "missing"
    end

    # MINOR-2 coverage: wrong token → invalid.
    test "rejects forged/incorrect token" do
      err =
        assert_raise GRPC.RPCError, fn ->
          call_with_token("not-the-real-secret")
        end

      assert err.status == GRPC.Status.unauthenticated()
      assert err.message =~ "invalid"
    end

    # MINOR-2 coverage: correct token → next.(req, stream) is invoked.
    test "accepts correct token and forwards to next" do
      assert {:next_called, :fake_req} = call_with_token(@secret)
    end

    # Fail-secure: when no secret is configured, every request is rejected
    # (even one with a "matching" empty token). Locks in fail-secure default.
    test "fail-secure: rejects all requests when no secret configured" do
      Application.delete_env(:event_dispatcher, :internal_secret)

      err =
        assert_raise GRPC.RPCError, fn ->
          # Use a non-empty token so we exercise the secret-loading path,
          # not the empty-string short-circuit.
          call_with_token("any-token-at-all")
        end

      assert err.status == GRPC.Status.unauthenticated()
      assert err.message =~ "invalid"
    end
  end
end
