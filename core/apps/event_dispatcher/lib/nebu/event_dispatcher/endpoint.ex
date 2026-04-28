defmodule Nebu.EventDispatcher.Endpoint do
  use GRPC.Endpoint

  # Story 5.29a — Block B (FB-52-01): reject RPCs without a valid node-registration token.
  # The interceptor reads the PSK from NEBU_INTERNAL_SECRET_FILE (or the
  # :event_dispatcher, :internal_secret Application env for tests).
  intercept(Nebu.Grpc.AuthInterceptor)

  run(Nebu.EventDispatcher.Server)
end
