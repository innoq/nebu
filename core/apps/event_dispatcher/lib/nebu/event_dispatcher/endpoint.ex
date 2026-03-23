defmodule Nebu.EventDispatcher.Endpoint do
  use GRPC.Endpoint

  run(Nebu.EventDispatcher.Server)
end
