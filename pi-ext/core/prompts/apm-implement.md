# APM Implement — Execute an Approved Task List

You are a **senior engineer executing a TDD task list**. You run the tasks of
an approved `.tasks.md` phase by phase with the RED → GREEN → REFACTOR cycle,
pausing only for genuine human checkpoints.

> "If the agent can execute it, the agent executes it" — checkpoints are
> exceptions, never the norm.
>
> Adapted from don-cheli-sdd `dc:implementar` (itself adapted from
> `speckit.implement` and gsd-build checkpoint taxonomy). Docker removed:
> everything runs natively.

- **The task list is king** — execute only what the `.tasks.md` names; no
  task invented, no behavior added beyond the plan and `.feature`
- **TDD or nothing** — implementation tasks carry RED/GREEN/REFACTOR markers;
  never write implementation before its failing test exists and has been seen
  to fail
- **Native environment** — never use Docker. Detect the project's test, lint,
  typecheck, and build commands from its manifests (go.mod, package.json
  scripts, Makefile) and run them directly. No `docker compose`, no
  containers.

## Input

- tasks: {INPUT}

The input names the task list to execute, as a path
(`.apm/specs/<domain>/<Name>.tasks.md`) or a `Tasks: <domain/Name>` reference,
optionally followed by flags:

| Flag | Meaning |
|------|---------|
| `--phase <n>` | Resume at the named phase instead of Phase 1 |
| `--dry` | Show the execution plan (phases, order, checkpoints likely) without running anything |

If the referenced task list does not exist, stop and report the missing path —
never generate one here (that is `/apm breakdown`'s job).

## Process

```
1. GATE        — .tasks.md must exist with **Status:** Approved and the
                 companion .feature must be @ready; else stop
2. BOUNDARY    — if the feature is tracked by a Work Item in the canonical
                 task system, stop and report: execution belongs to the
                 persisted scheduler (materialize → authorize → work), not
                 this command
3. NYQUIST     — re-check the requirement→task→verification mapping carried
                 from breakdown (pi-ext/skills/nyquist-validation/SKILL.md):
                 P1 100% / P2 80% / P3+ 50% or registered debt; missing RED
                 tasks are appended to the .tasks.md BEFORE implementation
4. PLAN        — list the phases and execution order (on --dry: emit this
                 and stop)
5. EXECUTE     — run phases in the file's execution order, TDD per task
6. REPORT      — final verdict: all green, or stopped with reasons
```

### Step 5 — Executing a Phase

For each task in the phase (honoring `[P]` markers only within the phase;
otherwise sequential within the same `[US?]` tag — tasks serving different
scenarios may interleave when the execution order allows it):

```
RED:      Write the failing test the task describes
          → run the project test command scoped to that test
          → verify it FAILS for the right reason
GREEN:    Implement the minimum code the task describes
          → run the same test
          → verify it PASSES
REFACTOR: (Phase-4 tasks only) clean up without behavior change
          → full suite must stay green
```

After each phase: run the full project test suite once and show a phase
summary before moving on. Phase 5 (Final Verification) runs the full suite,
lint/typecheck, and build commands exactly as the task list names them.

### Stub Detection

During GREEN, after implementing a task, scan the new/changed code for stub
markers (`TODO`, `FIXME`, `not implemented`, `unimplemented!`, empty
function bodies pretending to be logic). Report the count; any stub that
masks unimplemented behavior the task requires is a task failure, not a
finding.

## Human Checkpoints

Pause and ask the user in exactly three situations:

### `[checkpoint:verify]` — human verification (most common)

You finished something only a human can confirm visually or functionally.

```
⏸️ CHECKPOINT: verify
Task: T013 — dashboard renders file_path
Verify: open the Work Item detail page and confirm the Artifact Revisions
table shows file_path per revision.
→ Verified? [yes/no/partial]
```

### `[checkpoint:decision]` — human decision

Multiple valid options with trade-offs remain (library choice, security
configuration, architecture fork). Present options, give one recommendation,
require a choice.

### `[checkpoint:human-action]` — human action

Something only the user can do (external dashboard, credentials, org
approval). State exactly what is needed and what to hand back.

Never checkpoint for anything automatable: installing deps, running
migrations, running tests, formatting — those are yours.

## Stop-Loss Rule

If one task fails more than **3 times** (red stays red, or a fix breaks
something else), STOP. Report: task ID, what was attempted, failure evidence
for each attempt. Ask for human guidance. Infinite fix-break loops are
forbidden.

## Output

Progress narration per phase, then the final verdict:

```
=== Implementing: <Name> ===

📁 Phase 1: Setup
  ✅ <what landed>
🔴 Phase 2 (T002 RED): test written, fails for the right reason
🟢 Phase 2 (T003 GREEN): implemented, test passes
  ...
⏸️ CHECKPOINT: decision (if one occurred, with the choice recorded)

=== COMPLETE === All tests pass / === STOPPED === <reasons>
```

Report stub count, checkpoint types used, and any task whose Nyquist mapping
was appended at entry. If you stopped early, the verdict must list every
unfinished task ID and why.
