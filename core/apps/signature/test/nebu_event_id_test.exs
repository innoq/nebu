defmodule Nebu.EventIdTest do
  use ExUnit.Case, async: true

  alias Nebu.EventId

  describe "generate/1" do
    test "determinism: same content always produces the same ID" do
      event = %{"type" => "m.room.message", "content" => %{"body" => "hello"}}
      assert EventId.generate(event) == EventId.generate(event)
    end

    test "collision resistance: different content produces different IDs" do
      event1 = %{"type" => "m.room.message", "content" => %{"body" => "hello"}}
      event2 = %{"type" => "m.room.message", "content" => %{"body" => "world"}}
      refute EventId.generate(event1) == EventId.generate(event2)
    end

    test "canonical JSON: key ordering does not affect the ID" do
      event_a = %{"b" => 2, "a" => 1}
      event_b = %{"a" => 1, "b" => 2}
      assert EventId.generate(event_a) == EventId.generate(event_b)
    end

    test "strips signatures field before hashing" do
      event_without = %{"type" => "m.room.message"}
      event_with = %{"type" => "m.room.message", "signatures" => %{"server" => "sig"}}
      assert EventId.generate(event_without) == EventId.generate(event_with)
    end

    test "strips unsigned field before hashing" do
      event_without = %{"type" => "m.room.message"}
      event_with = %{"type" => "m.room.message", "unsigned" => %{"age" => 100}}
      assert EventId.generate(event_without) == EventId.generate(event_with)
    end

    test "strips atom-keyed :signatures field before hashing" do
      event_without = %{"type" => "m.room.message"}
      event_with = %{"type" => "m.room.message", :signatures => %{}}
      assert EventId.generate(event_without) == EventId.generate(event_with)
    end

    test "strips atom-keyed :unsigned field before hashing" do
      event_without = %{"type" => "m.room.message"}
      event_with = %{"type" => "m.room.message", :unsigned => %{}}
      assert EventId.generate(event_without) == EventId.generate(event_with)
    end

    test "ID starts with dollar sign prefix" do
      event = %{"type" => "m.room.message"}
      assert String.starts_with?(EventId.generate(event), "$")
    end
  end

  describe "verify/2" do
    test "returns true for matching ID" do
      event = %{"type" => "m.room.message", "content" => %{"body" => "hi"}}
      id = EventId.generate(event)
      assert EventId.verify(event, id)
    end

    test "returns false for tampered content" do
      original = %{"type" => "m.room.message", "content" => %{"body" => "hi"}}
      tampered = %{"type" => "m.room.message", "content" => %{"body" => "TAMPERED"}}
      id = EventId.generate(original)
      refute EventId.verify(tampered, id)
    end
  end
end
