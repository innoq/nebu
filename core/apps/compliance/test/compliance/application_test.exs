defmodule Compliance.ApplicationTest do
  use ExUnit.Case, async: false

  # Story 5-2 AC2 — Compliance OTP App boots as a stateless umbrella app.
  # AuditWriter is a pure module function, so there is no GenServer to supervise;
  # the Application.start/2 callback returns a supervisor with an empty children
  # list. This test locks that contract in so a future accidental GenServer
  # addition doesn't silently slip past AC2's Option C classification.

  test "compliance app starts and is listed in started_applications" do
    {:ok, _started} = Application.ensure_all_started(:compliance)
    started_names = Enum.map(Application.started_applications(), fn {name, _, _} -> name end)
    assert :compliance in started_names
  end

  test "Compliance.Application exposes an empty children list (Option C stateless)" do
    # Reach into the supervisor started under :compliance app.
    {:ok, _} = Application.ensure_all_started(:compliance)
    sup_pid = Process.whereis(Compliance.Supervisor)

    assert is_pid(sup_pid),
           "Compliance.Supervisor should be running under the :compliance app"

    children = Supervisor.which_children(sup_pid)

    assert children == [],
           "AC2 (Option C stateless) requires Compliance.Application children=[]. " <>
             "Got: #{inspect(children)}. If a genuine GenServer worker is needed, " <>
             "update AC2 in the story and add the corresponding crash/restart test."
  end
end
