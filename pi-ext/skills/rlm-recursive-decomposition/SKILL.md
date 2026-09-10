---
name: rlm-recursive-decomposition
description: "Recursive decomposition with subagent delegation for massive inputs or very large codebases; the orchestrator inspects data programmatically, never ingests it whole. Activate when the user says: 'rlm decomposition', 'massive context', 'huge codebase', 'input too large', or when data size alone would degrade reasoning quality."
version: 1.0.0
adopted_from: don-cheli-sdd comandos/razonar (v1.0.0, Don Cheli)
tags: [reasoning, rlm, decomposition, scale]
---

# RLM: Recursive Decomposition with Sub-LLMs

For massive inputs or extremely long contexts: decompose programmatically and
delegate sub-tasks to fresh subagents in parallel. The orchestrator never
ingests the data whole.

> "Instead of directly ingesting its (potentially enormous) input data, the
> RLM uses a persistent external environment to inspect and transform its
> input, and calls sub-LLMs from that environment." — PrimeIntellect

## Adaptation to this environment

The external environment is the **filesystem + grep/rg + Agent spawns**.

## Process

1. **Measure** the input first (`rg --count`, `wc -l`, `find | wc -l`) —
   know the size before reading anything.
2. **Sample** small slices (`head`, targeted `rg` with context) to build a
   structural map — file tree, section headers, symbol index. The
   orchestrator sees the map, not the content.
3. **Partition** into self-contained chunks (per module, per directory,
   per section) that can be reasoned about independently.
4. **Delegate** each chunk to a fresh background Agent spawn with the
   chunk scope and the specific question — in parallel when independent.
5. **Reduce**: each subagent returns a bounded summary; the orchestrator
   composes the final answer from summaries + the structural map.
6. **Recurse**: a chunk too large for one subagent gets the same
   measure → sample → partition → delegate treatment.

## Rules

- Never `cat` a massive file into the orchestrator context.
- Never let one subagent's prompt carry another subagent's raw output.
- Failed or inconsistent chunk results are re-delegated with narrower scope,
  not absorbed for manual judgment in the polluted main context.
