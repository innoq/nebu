defmodule Nebu.Grpc.MetadataTest do
  use ExUnit.Case, async: true
  alias Nebu.Grpc.Metadata

  defp build_stream(headers) do
    %{adapter: %{payload: %{headers: headers}}}
  end

  test "user_id/1 returns value when header present" do
    stream = build_stream([{"x-user-id", "@alice:example.com"}])
    assert Metadata.user_id(stream) == "@alice:example.com"
  end

  test "user_id/1 returns nil when header absent" do
    stream = build_stream([])
    assert Metadata.user_id(stream) == nil
  end

  test "system_role/1 returns value when present" do
    stream = build_stream([{"x-system-role", "instance_admin"}])
    assert Metadata.system_role(stream) == "instance_admin"
  end

  test "system_role/1 defaults to user when absent" do
    stream = build_stream([])
    assert Metadata.system_role(stream) == "user"
  end

  test "trusted_identity/1 returns both values" do
    stream = build_stream([{"x-user-id", "@kai:example.com"}, {"x-system-role", "instance_admin"}])
    assert Metadata.trusted_identity(stream) == {"@kai:example.com", "instance_admin"}
  end
end
