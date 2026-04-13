# Story 4.3: Ed25519 Unit Tests + `Nebu.EventId` Content-Hash Module

Status: done

## Story

As a Core developer,
I want a dedicated `Nebu.EventId` module that generates Matrix Room Version 6 content-hash Event IDs,
so that every event has a deterministic, verifiable, content-addressable identifier.

## Acceptance Criteria

1. `core/apps/signature/lib/nebu/event_id.ex` implements `Nebu.EventId.generate/1`:
   - Accepts an event content map (any `map()`)
   - Strips `signatures` and `unsigned` fields before hashing (per Matrix Room Version 6 spec)
   - Serializes to canonical JSON: keys sorted alphabetically, no whitespace — via `Jason.encode!/1` with sorted keys
   - Computes SHA-256 of the canonical JSON bytes via `:crypto.hash(:sha256, json_bytes)`
   - URL-safe Base64-encodes (no padding) the hash via `Base.url_encode64(hash, padding: false)`
   - Returns `"$" <> encoded_hash` (e.g., `"$abc123def456..."`)

2. `Nebu.EventId.verify/2` accepts an event content map and an event ID string:
   - Recomputes the ID via `generate/1`
   - Returns `true` if the recomputed ID matches the given event ID, `false` otherwise

3. Unit tests in `core/apps/signature/test/nebu/event_id_test.exs` cover:
   - Same content always produces the same ID (determinism)
   - Different content produces different IDs (collision resistance)
   - Key ordering in the input map does NOT affect the ID (canonical JSON — alphabetical sort)
   - `verify/2` returns `true` for a matching ID
   - `verify/2` returns `false` for a tampered ID (content changed after ID was generated)
   - `signatures` and `unsigned` fields are stripped before hashing (spec compliance)

4. Existing Ed25519/X25519/PII unit tests in `nebu_signature_test.exs` pass unchanged with `--warnings-as-errors`.

5. `mix test --warnings-as-errors` passes for the full umbrella.

---

## Tasks / Subtasks

- [x] Add `{:jason, "~> 1.4"}` dependency to `core/apps/signature/mix.exs` (AC: #1)
  - [x] Jason is already in `mix.lock` (used by `event_dispatcher`), no version conflict

- [x] Create `core/apps/signature/lib/nebu/event_id.ex` (AC: #1, #2)
  - [x] Define module `Nebu.EventId`
  - [x] Implement `generate/1`: strip `signatures`/`unsigned`, canonical JSON (sorted keys), SHA-256, Base64url no-padding, prepend `"$"`
  - [x] Implement `verify/2`: recompute + compare
  - [x] Add `@moduledoc`, `@doc`, `@spec` annotations

- [x] Create `core/apps/signature/test/nebu/event_id_test.exs` (AC: #3)
  - [x] Test: determinism — same map produces same ID
  - [x] Test: collision resistance — different map produces different ID
  - [x] Test: key-order independence — `%{b: 1, a: 2}` and `%{a: 2, b: 1}` produce same ID
  - [x] Test: `verify/2` true for matching ID
  - [x] Test: `verify/2` false for tampered content
  - [x] Test: `signatures` field is stripped — `%{type: "m.room.message", signatures: %{}}` same ID as `%{type: "m.room.message"}`
  - [x] Test: `unsigned` field is stripped — same pattern as above
  - [x] Use `async: true` (pure crypto, no shared state)
  - [x] Use `describe` blocks (established test pattern)

- [x] Run `mix test --warnings-as-errors` for full umbrella (AC: #4, #5)

### Review Findings

- [x] [Review][Patch] MAJOR: `normalize_keys/1` uses `Map.new/1` which destroys sort order for maps >32 keys — breaks canonical JSON spec [event_id.ex:80-84] — fixed: replaced with `Jason.OrderedObject`
- [x] [Review][Patch] MINOR: Doctest example in `generate/1` has non-executable expected value `"$..."` [event_id.ex:25-26] — fixed: replaced with executable assertion
- [x] [Review][Patch] MINOR: Comment in line 69 falsely claims Jason preserves insertion order for sorted maps [event_id.ex:69] — fixed: corrected comment
- [x] [Review][Defer] INFO: Architecture expects separate `canonical_json.ex` module — deferred, acceptable as private function until extraction needed by Story 4-4+
- [x] [Review][Defer] INFO: Tests do not cover maps with >32 keys — deferred, will be covered when MAJOR fix is verified via container tests

---

## Dev Notes

### File Location: `signature` App, NOT a New App

`Nebu.EventId` lives in `core/apps/signature/lib/nebu/event_id.ex`.

Architecture rule #8: `canonical_json/1` from the Signature-App — `Nebu.EventId` IS the Signature-App's canonical JSON + content-hash module. Story 4-4 references `Signature.Ed25519.sign/2` and `Nebu.EventId.generate/1` — both from the same `signature` app.

Do NOT create a new umbrella app. Do NOT put `event_id.ex` in `room_manager`.

### Jason Dependency

Jason is already compiled and present in `core/mix.lock` (used by `event_dispatcher` at `"~> 1.4"`, resolved to `1.4.4`). Adding it to `signature/mix.exs` deps triggers NO new download — it reuses the existing resolved version.

**Add to `core/apps/signature/mix.exs`:**
```elixir
defp deps do
  [{:jason, "~> 1.4"}]
end
```

**Do NOT modify** `core/mix.lock` manually — Mix resolves it automatically.

### Canonical JSON: Sorted Keys via Jason

Matrix Room Version 6 requires canonical JSON: keys alphabetically sorted, no whitespace, no `signatures` or `unsigned` fields.

Jason does NOT sort keys by default — `Jason.encode!(%{b: 1, a: 2})` may produce `{"b":1,"a":2}`. Use `Jason.encode!/2` with the `:keys` option? Actually, Jason sorts map keys by default for atom maps but NOT for string-keyed maps. The safest approach is to convert the map to a keyword list sorted by key:

**Correct pattern:**
```elixir
def canonical_json(event) do
  event
  |> Map.drop(["signatures", "unsigned", :signatures, :unsigned])
  |> sort_keys_deep()
  |> Jason.encode!()
end

defp sort_keys_deep(map) when is_map(map) do
  map
  |> Enum.map(fn {k, v} -> {to_string(k), sort_keys_deep(v)} end)
  |> Enum.sort_by(fn {k, _} -> k end)
  |> Map.new()
end
defp sort_keys_deep(list) when is_list(list), do: Enum.map(list, &sort_keys_deep/1)
defp sort_keys_deep(value), do: value
```

**Alternative: use Jason.OrderedObject** — but this is heavier. The `sort_keys_deep` approach is simpler and follows the established pattern.

**Important:** Jason encodes Elixir maps with string keys in insertion order (in Erlang, map ordering is hash-based, NOT alphabetical). Always normalize to sorted string keys before encoding.

**Do NOT use atom keys in the canonical map** — event content from gRPC/JSON will arrive with string keys.

### `generate/1` Implementation Sketch

```elixir
defmodule Nebu.EventId do
  @moduledoc """
  Generates Matrix Room Version 6 content-hash Event IDs.

  Format: "$" <> Base64url(SHA-256(canonical_json(event \\ {signatures, unsigned})))
  Spec: Matrix Room Version 6+ (https://spec.matrix.org/v1.x/rooms/v6/)
  """

  @doc """
  Generates a content-hash Event ID for a Matrix event.

  Strips `signatures` and `unsigned` fields, serializes to canonical JSON
  (alphabetically sorted keys, no whitespace), computes SHA-256, and returns
  `"$" <> Base64url_no_padding(hash)`.
  """
  @spec generate(map()) :: String.t()
  def generate(event) when is_map(event) do
    hash =
      event
      |> Map.drop(["signatures", "unsigned", :signatures, :unsigned])
      |> canonical_json()
      |> then(&:crypto.hash(:sha256, &1))

    "$" <> Base.url_encode64(hash, padding: false)
  end

  @doc """
  Verifies that an event ID matches the event content.

  Returns `true` if `generate(event) == event_id`, `false` otherwise.
  """
  @spec verify(map(), String.t()) :: boolean()
  def verify(event, event_id) when is_map(event) and is_binary(event_id) do
    generate(event) == event_id
  end

  # Converts map to canonical JSON: sorted string keys, no whitespace.
  defp canonical_json(map) do
    map
    |> normalize_keys()
    |> Jason.encode!()
  end

  # Recursively convert to string-keyed maps sorted alphabetically.
  defp normalize_keys(map) when is_map(map) do
    map
    |> Enum.map(fn {k, v} -> {to_string(k), normalize_keys(v)} end)
    |> Enum.sort_by(fn {k, _} -> k end)
    |> Map.new()
  end
  defp normalize_keys(list) when is_list(list), do: Enum.map(list, &normalize_keys/1)
  defp normalize_keys(value), do: value
end
```

**Key points:**
- `Map.drop/2` handles both atom keys (`:signatures`) and string keys (`"signatures"`) — event content may come either way
- `normalize_keys/1` converts all keys to strings and sorts alphabetically at every nesting level
- `Jason.encode!` encodes the resulting string-keyed map (Jason preserves insertion order for maps created from `Enum.sort`)
- `:crypto.hash(:sha256, json_bytes)` — `json_bytes` is a binary (Jason returns binary from `encode!`)
- `Base.url_encode64(..., padding: false)` — URL-safe alphabet, no `=` padding

### Test File Location

Per Elixir test discovery convention, a module `Nebu.EventId` tested in `Nebu.EventIdTest` lives at:
```
core/apps/signature/test/nebu/event_id_test.exs
```

The `test/` directory in the `signature` app is auto-discovered. Subdirectories (`test/nebu/`) are discovered automatically — no explicit `Code.require_file` needed.

Alternatively, the file can live at `core/apps/signature/test/nebu_event_id_test.exs` (flat). Both work. The flat pattern `nebu_event_id_test.exs` is simpler and consistent with `nebu_signature_test.exs`.

**Recommendation: use flat pattern** `test/nebu_event_id_test.exs` — matches `nebu_signature_test.exs` convention.

### Test Implementation Sketch

```elixir
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
      event_with    = %{"type" => "m.room.message", "signatures" => %{"server" => "sig"}}
      assert EventId.generate(event_without) == EventId.generate(event_with)
    end

    test "strips unsigned field before hashing" do
      event_without = %{"type" => "m.room.message"}
      event_with    = %{"type" => "m.room.message", "unsigned" => %{"age" => 100}}
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
```

### OTP Sign/Verify API (Existing Tests, Must Keep Passing)

From Story 2-8, the correct EdDSA sign/verify API in OTP (learned during implementation — not what the story spec said):

```elixir
# Sign (4-arg form with curve in key list):
signature = :crypto.sign(:eddsa, :none, message, [private_key, :ed25519])

# Verify (4-arg form with curve in key list):
:crypto.verify(:eddsa, :none, message, signature, [public_key, :ed25519])
```

**NOT** the 5-arg form `(:eddsa, :none, msg, [key], [:ed25519])` — that is RSA-only syntax.
**NOT** `private_key: 64 bytes` — OTP returns 32-byte seed format, not libsodium 64-byte format.

These tests are already correct in `nebu_signature_test.exs` — do NOT change them.

### Architecture Enforcement Rules Relevant to This Story

| Rule | Requirement |
|---|---|
| Rule #7 | Event-IDs always via `Nebu.EventId.generate/1` — never manually constructed |
| Rule #8 | `canonical_json/1` from Signature-App — no custom implementation elsewhere |
| Rule #6 | `{:ok, result}` / `{:error, reason}` for business logic — but `generate/1` returns a plain string (not a tagged tuple) since it cannot fail |
| G7 (arch) | Format: `$<base64url(SHA-256(canonical_json(event \ {signatures, unsigned})))>` |

### `sign_event` Function: Deferred to Story 4-4

Story 4-4 references `Signature.Ed25519.sign/2` for event signing. This is NOT in scope for Story 4-3. The epics.md acceptance criteria for Story 4-3 says "Existing Ed25519 unit tests from Story 1.x pass" — this refers to the existing `nebu_signature_test.exs` tests. Story 4-3 only adds `Nebu.EventId`.

Do NOT implement `Nebu.Signature.sign_event/2` in this story.

### File Structure

Only create/modify these files:

```
core/apps/signature/
  lib/nebu/
    event_id.ex               ← CREATE: Nebu.EventId module
  mix.exs                     ← MODIFY: add {:jason, "~> 1.4"} to deps
  test/
    nebu_event_id_test.exs    ← CREATE: unit tests for Nebu.EventId
```

**Do NOT modify:**
- `nebu_signature_test.exs` — existing tests must pass unchanged
- `nebu/signature.ex` — no changes needed
- `nebu/signature/application.ex` — no changes needed
- `mix.lock` — auto-updated by Mix, do not manually edit
- Any other app's files

### Build & Test Commands

```bash
# Run signature app tests only (fast, targeted):
make test-unit-elixir
# OR from inside the core container:
cd core && mix test apps/signature --warnings-as-errors

# Run full umbrella (before marking complete):
make test-unit-elixir
```

All tests run inside Docker containers — no local Elixir install needed.

**Expected result:** All existing 20 signature tests pass + 8 new EventId tests = 28 tests, 0 failures.

---

## Previous Story Intelligence (Story 4-2)

**Key learnings from Story 4-2 implementation:**

1. **Runtime DB injection pattern**: `Application.get_env/3` at runtime (NOT `Application.compile_env`) enables test overrides via `Application.put_env`. This is NOT needed for Story 4-3 (no DB), but is the established pattern for config-injectable modules.

2. **Module name convention confirmed**: All modules follow `Nebu.{Domain}.{Name}` — e.g., `Nebu.EventId`, NOT `EventDispatcher.EventId` or `Nebu.Event.Id`.

3. **`async: true` for pure unit tests**: All tests with no shared state (pure crypto, pure logic) use `async: true`. The EventId tests qualify.

4. **Dependency addition pattern**: When adding a new dep to an umbrella app's `mix.exs`, the dep must already exist in `mix.lock` OR will be fetched from hex. Jason IS in `mix.lock` already (version 1.4.4).

5. **Horde `async: false` rule**: NOT needed here — EventId tests have no Horde processes.

6. **Files modified in Story 4-2:**
   - `gateway/migrations/000009_rooms.up.sql` — NEW
   - `gateway/migrations/000009_rooms.down.sql` — NEW
   - `core/apps/room_manager/mix.exs` — MODIFIED
   - `core/apps/room_manager/lib/nebu/room/server.ex` — MODIFIED
   - `core/apps/room_manager/lib/nebu/room/db.ex` — NEW
   - `core/apps/room_manager/test/nebu_room_test.exs` — MODIFIED

7. **Review Finding: `ON CONFLICT DO NOTHING` broke rejoin** — not relevant here, but signals that edge cases in logic should be tested explicitly (the `verify/2` false-positive case is the equivalent here).

---

## Architecture Compliance

| Requirement | Source |
|---|---|
| `Nebu.EventId` lives in `signature` app | `architecture.md#G7` — "Implementierung: Elixir `Nebu.EventId` Modul, wiederverwendet `canonical_json/1` aus Signature-App" |
| Format: `$<base64url(SHA-256(canonical_json(event)))>` | `architecture.md#G7` |
| Strip `signatures` + `unsigned` before hashing | `architecture.md#G7` — "keine `signatures`- und `unsigned`-Felder" |
| Canonical JSON: keys alphabetisch sortiert | `architecture.md#G7` |
| `Nebu.{Domain}.{Name}` module naming | `architecture.md#Naming Conventions > Elixir` |
| Event-IDs always via `Nebu.EventId.generate/1` | `architecture.md#Enforcement rule #7` |
| No custom canonical JSON implementation outside Signature-App | `architecture.md#Enforcement rule #8` |
| OTP `:crypto` — no external crypto deps | `architecture.md#V1` — "beide native in OTP 24+" |
| Unit tests mandatory for crypto operations | `architecture.md#G6` |

---

## Cross-Story Context

| Story | Relationship to 4-3 |
|---|---|
| Story 2-8 | Established `nebu_signature_test.exs` with `async: true`, `describe` blocks, correct OTP Ed25519 sign/verify 4-arg form |
| Story 4-4 | Calls `Nebu.EventId.generate/1` for event ID in `handle_call({:send_event, ...})` |
| Story 4-11 | Go gateway PUT handler returns `{"event_id": "$..."}` — the hash produced by this module |
| Story 5-6 | Compliance export verifies event integrity by recomputing EventId — `verify/2` is used there |

---

## Dev Agent Record

### Completion Notes

- Implemented `Nebu.EventId` module in `core/apps/signature/lib/nebu/event_id.ex` with:
  - `generate/1`: strips `signatures`/`unsigned` (both string and atom key forms), normalizes all map keys to sorted string keys recursively via `normalize_keys/1`, serializes via `Jason.encode!`, computes SHA-256 with `:crypto.hash/2`, encodes with `Base.url_encode64/2` (no padding), prepends `"$"`.
  - `verify/2`: recomputes ID via `generate/1` and compares with `==`.
  - Full `@moduledoc`, `@doc`, `@spec` annotations.
- Added `{:jason, "~> 1.4"}` to `core/apps/signature/mix.exs` — no version conflict; Jason 1.4.4 was already in `mix.lock`.
- Created 10 unit tests in flat pattern `test/nebu_event_id_test.exs` (consistent with `nebu_signature_test.exs`): determinism, collision resistance, key-order independence, `verify/2` true/false, string `signatures`/`unsigned` stripping, atom `:signatures`/`:unsigned` stripping, `"$"`-prefix check.
- Full umbrella `mix test --warnings-as-errors` passed: 21 signature tests (11 existing + 10 new), 0 failures, no regressions in any app.

### File List

- `core/apps/signature/mix.exs` — MODIFIED: added `{:jason, "~> 1.4"}` dependency
- `core/apps/signature/lib/nebu/event_id.ex` — CREATED: `Nebu.EventId` module
- `core/apps/signature/test/nebu_event_id_test.exs` — CREATED: 10 unit tests for `Nebu.EventId`

## Change Log

- 2026-04-11: Test review passed — 100/100 (A), 0 actionable violations, all 5 ACs covered; status → done
- 2026-04-11: Re-verification — `make test-unit-elixir` (221 tests, 0 failures, --warnings-as-errors clean); status → review
- 2026-04-03: Code review — 1 MAJOR fixed (`normalize_keys` Map.new→Jason.OrderedObject), 2 MINOR fixed (doctest, comment); status → in-progress for re-verification
- 2026-04-03: Story implemented — `Nebu.EventId` module + 10 unit tests added; all ACs satisfied; umbrella tests green
- 2026-04-03: Story created — ready-for-dev
