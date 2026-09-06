# Plan: APM Implement Handoff

## Status
Approved

## Summary
Prompt-only redesign of `/apm implement`. The prompt file
`pi-ext/core/prompts/apm-implement.md` is rewritten so its Process becomes:

```
1. GATE        — unchanged: Approved .tasks.md + companion .plan.md +
                 @ready .feature, else stop
2. IMPORT      — dry-run `pic workflow import-apm <tasks.md> --milestone
                 <version>` (ask owner for the milestone/PRD version if not
                 supplied), show the parsed graph, then run the real import;
                 on the re-import hard stop ("already imported: epic wi-…"),
                 switch to RESUME mode: report epic ID, per-feature child
                 states, next ready work (via `pic show <epic-id>`)
3. AUTHORIZ.   — relay the owner gate: ask in conversation; on explicit yes
                 call authorize_work_item_implementation with
                 actor_role=owner; never self-authorize
4. HANDOFF     — exit with a completion summary (epic ID, branch, how to
                 check status on demand); no polling loop
```

The old BOUNDARY step and every inline-execution section (Step 5 phase
execution, TDD cycle, stub detection, stop-loss for implementation) are
removed: execution belongs to the scheduler. Human-checkpoint taxonomy is
kept only where it applies to the handoff itself (decision: milestone
version; human-action: owner authorization).

## Invariants check

- Canonical workflow preserved: import creates Work Items only through
  `pic workflow import-apm`; authorize goes through
  `authorize_work_item_implementation`; no direct DB writes; gates unchanged.
- Nyquist still applies: implement re-checks the mapping carried in the
  epic description (importer embeds it) and reports coverage at entry —
  re-check, never re-derive.
- Standalone inline execution is removed on purpose (owner decision
  2026-09-06): untracked features become tracked; ad-hoc fixes never
  needed /apm implement.
- `apm-command.test.ts` pins `- tasks: {INPUT}` in apm-implement.md and
  regexes over apm-implement.ts — the Input contract and the .ts stay
  untouched.

## API / surface

- Prompt: `pi-ext/core/prompts/apm-implement.md` (rewrite Process, remove
  execution sections, keep Input contract, flags `--dry` reinterpreted as
  "show import plan and stop" is dropped — dry-run preview is what the
  importer's `--dry-run` already provides; implement just invokes it).
- Prompt: `pi-ext/core/prompts/apm-breakdown.md` — handoff section updated
  to describe implement as import + scheduler handoff.
- No code, schema, or CLI changes.

## Verification strategy
- Content checks: rg assertions that the new steps exist and the old
  inline-execution instructions are gone.
- Suite: `cd pi-ext && pnpm test` stays green (apm-command.test.ts
  unchanged).
