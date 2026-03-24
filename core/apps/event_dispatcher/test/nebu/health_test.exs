defmodule Nebu.HealthTest do
  use ExUnit.Case, async: true

  describe "check/0" do
    test "returns UP status when all components are healthy" do
      health = Nebu.Health.check()
      assert health.status == "UP"
    end

    test "returns correct shape for UP state" do
      health = Nebu.Health.check()
      assert health.load_factor == 1.0
      assert health.version == "0.1.0"
      assert is_binary(health.node)
      assert is_map(health.components)
    end

    test "components contain database with UP status" do
      health = Nebu.Health.check()
      assert health.components.database.status == "UP"
    end

    test "room_registry component has room_count 0" do
      health = Nebu.Health.check()
      assert health.components.room_registry.status == "UP"
      assert health.components.room_registry.room_count == 0
    end

    test "event_bus component has connected_gateways 0" do
      health = Nebu.Health.check()
      assert health.components.event_bus.status == "UP"
      assert health.components.event_bus.connected_gateways == 0
    end

    test "load_factor is always 1.0 in MVP" do
      assert Nebu.Health.check().load_factor == 1.0
    end

    test "version is 0.1.0" do
      assert Nebu.Health.check().version == "0.1.0"
    end

    test "node field is a non-empty string" do
      node_str = Nebu.Health.check().node
      assert is_binary(node_str)
      assert byte_size(node_str) > 0
    end
  end

  describe "overall_status/1" do
    test "returns UP when all components are UP" do
      components = %{
        database: %{status: "UP"},
        room_registry: %{status: "UP"},
        event_bus: %{status: "UP"}
      }

      assert Nebu.Health.overall_status(components) == "UP"
    end

    test "returns DEGRADED when a component is DEGRADED" do
      components = %{
        database: %{status: "UP"},
        room_registry: %{status: "DEGRADED"},
        event_bus: %{status: "UP"}
      }

      assert Nebu.Health.overall_status(components) == "DEGRADED"
    end

    test "returns DOWN when a component is DOWN" do
      components = %{
        database: %{status: "DOWN"},
        room_registry: %{status: "UP"},
        event_bus: %{status: "UP"}
      }

      assert Nebu.Health.overall_status(components) == "DOWN"
    end

    test "DOWN takes priority over DEGRADED" do
      components = %{
        database: %{status: "DOWN"},
        room_registry: %{status: "DEGRADED"},
        event_bus: %{status: "UP"}
      }

      assert Nebu.Health.overall_status(components) == "DOWN"
    end
  end
end
