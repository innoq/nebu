defmodule Nebu.Repo do
  use Ecto.Repo,
    otp_app: :nebu_db,
    adapter: Ecto.Adapters.Postgres
end
