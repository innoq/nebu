defmodule Nebu.Signature.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Placeholder — Ed25519/X25519 signing processes added in Epic 4
    ]

    opts = [strategy: :one_for_one, name: Nebu.Signature.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
