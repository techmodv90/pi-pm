---
name: rlm-chain-of-thought
description: "Multi-step reasoning with context folding: delegate each step to fresh subagents, retain only concise summaries, avoid context rot. Activate when the user says: 'context folding', 'context rot', 'long multi-step reasoning', or when a reasoning chain degrades as steps accumulate."
version: 1.0.0
adopted_from: don-cheli-sdd comandos/razonar (v1.0.0, Don Cheli)
tags: [reasoning, rlm, context, multi-step]
---

# RLM: Chain of Thought with Context Folding

Context rot: in multi-step reasoning each step accumulates in context and
answer quality degrades. Context folding fixes it by delegation.

## Adaptation to this environment

- The main session decomposes the problem into steps and does NOT keep
  running full intermediate state in its own reasoning.
- Each step runs in a **fresh Agent spawn** with only the inputs that step needs.
- The orchestrator retains **concise summaries** of each step's result —
  not transcripts, not raw exploration.
- Never summarize a long context to shrink it (lossy); instead delegate
  programmatically: give the fresh subagent the original sources it needs.

## Process

1. Decompose the problem into an ordered chain of steps.
2. For each step: spawn a fresh Agent with (a) the step question, (b) the
   minimal original source material, (c) the previous steps' SUMMARIES only.
3. Reduce each result to a summary of conclusions before continuing.
4. Compose the final answer from the summaries.

## Rule

If a step needs more raw detail than a summary carries, re-delegate with the
original source — never try to reconstruct detail from memory of a
polluted context.
