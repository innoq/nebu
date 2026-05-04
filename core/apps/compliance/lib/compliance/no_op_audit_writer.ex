defmodule Compliance.NoOpAuditWriter do
  @moduledoc """
  No-op implementation of the audit writer interface.

  Used in test environments (configured via `:compliance, :audit_writer` in test.exs)
  to prevent audit log calls from requiring a live Nebu.Repo connection.

  Production always uses Compliance.AuditWriter (the default).
  """

  def log(_, _, _, _, _, _, _ \\ nil), do: :ok
end
