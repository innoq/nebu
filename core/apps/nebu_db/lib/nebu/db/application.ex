defmodule Nebu.DB.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children =
      if Application.get_env(:nebu_db, Nebu.Repo) do
        [Nebu.Repo]
      else
        []
      end

    opts = [strategy: :one_for_one, name: Nebu.DB.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
