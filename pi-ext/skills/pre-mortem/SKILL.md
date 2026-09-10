---
name: pre-mortem
description: "Anticipate failure BEFORE it happens: imagine the project already failed and analyze why. Activate when the user says: 'pre-mortem', 'what could go wrong', 'risk analysis', or when a plan/feature is about to be committed and failure modes need mitigation plans."
version: 1.0.0
adopted_from: don-cheli-sdd comandos/razonar (v1.0.0, Don Cheli)
tags: [reasoning, perspective, risk, planning]
---

# Pre-Mortem

Anticipate failure BEFORE it happens.

## Process

1. **Imagine** the project has already failed completely.
2. Ask: "Why did it fail?"
3. **List** all plausible causes.
4. **Assess** probability and impact of each.
5. **Create** a mitigation plan for the most probable/impactful ones.

Run before committing to a plan; combine with inversion for concrete failure mechanics.
