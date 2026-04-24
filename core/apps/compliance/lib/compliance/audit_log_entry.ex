defmodule Compliance.AuditLogEntry do
  use Ecto.Schema
  import Ecto.Changeset

  @moduledoc """
  Ecto schema for the audit_log table (created by migration 000018, Story 5-1).
  event_time has DEFAULT NOW() in the DB — no inserted_at/updated_at macros.
  """

  @primary_key false
  @timestamps_opts false

  schema "audit_log" do
    field :actor_user_id, :string
    field :action,        :string
    field :target_type,   :string
    field :target_id,     :string
    field :metadata,      :map
    field :outcome,       :string
    field :error_detail,  :string
  end

  @required_fields [:actor_user_id, :action, :outcome]
  @optional_fields [:target_type, :target_id, :metadata, :error_detail]

  def changeset(entry, attrs) do
    entry
    |> cast(attrs, @required_fields ++ @optional_fields)
    |> validate_required(@required_fields)
  end
end
