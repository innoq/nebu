defmodule Nebu.Grpc.Metadata do
  @moduledoc """
  Extracts validated user identity from incoming gRPC stream metadata.

  Elixir trusts these values fully — the Go gateway has already validated the OIDC token.
  No re-validation in Elixir (Architecture Rule: auth token never forwarded to Elixir).
  """

  @user_id_key "x-user-id"
  @system_role_key "x-system-role"
  @default_role "user"

  @doc "Returns x-user-id from gRPC stream metadata, or nil if absent."
  @spec user_id(GRPC.Server.Stream.t()) :: String.t() | nil
  def user_id(stream), do: get_header(stream, @user_id_key)

  @doc "Returns x-system-role from gRPC stream metadata. Defaults to \"user\" if absent."
  @spec system_role(GRPC.Server.Stream.t()) :: String.t()
  def system_role(stream), do: get_header(stream, @system_role_key) || @default_role

  @doc "Returns {user_id, system_role} tuple. system_role defaults to \"user\" if absent."
  @spec trusted_identity(GRPC.Server.Stream.t()) :: {String.t() | nil, String.t()}
  def trusted_identity(stream), do: {user_id(stream), system_role(stream)}

  defp get_header(stream, key) do
    headers = stream.adapter.payload.headers

    case List.keyfind(headers, key, 0) do
      {^key, value} -> value
      nil -> nil
    end
  end
end
