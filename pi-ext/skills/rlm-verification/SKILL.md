---
name: rlm-verification
description: "Verify work against specs by delegating to fresh subagents with clean context instead of checking everything in one polluted context. Activate when the user says: 'rlm verification', 'verify against spec', 'subagent verification', or when a large artifact needs independent multi-angle verification."
version: 1.0.0
adopted_from: don-cheli-sdd comandos/razonar (v1.0.0, Don Cheli)
tags: [reasoning, rlm, verification, subagents]
---

# RLM: Verification with Sub-LLMs

Based on the Recursive Language Model (RLM) paradigm (PrimeIntellect): the
main context stays clean and orchestrates; heavy verification work is
delegated to fresh sub-LLMs.

## Adaptation to this environment

The Python-REPL pattern maps to the **Agent tool** (pi-subagents-lite):
- The main session is the orchestrator — it does NOT verify inline.
- Each verification angle is a background Agent spawn (task-worker/reviewer persona).
- `worktree_path` isolates the verification environment.
- Transcripts land in /tmp/pi-agent-outputs/<agentId>.log — the orchestrator
  reads final results, never the full intermediate context.

## Process

1. **Define** verifiable criteria (spec lines, acceptance commands, thresholds).
2. **Decompose** verification into independent angles (run tests; review code vs spec; check docs vs behavior).
3. **Spawn** one fresh Agent per angle, in parallel, with the criteria and the file scope — nothing else.
4. **Collect** verdicts; only contradictions or failures return to the orchestrator for judgment.
5. **Finalize** only when every angle's result is recorded (fail-closed, like the verification-gate skill).

## Rule

The orchestrator never "takes a quick look itself" — that is exactly the
context pollution RLM exists to avoid.
