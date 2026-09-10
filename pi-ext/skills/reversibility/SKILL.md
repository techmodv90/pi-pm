---
name: reversibility
description: "Calibrate commitment level by whether a decision is reversible or irreversible (one-way vs two-way doors). Activate when the user says: 'reversible', 'one-way door', 'how careful should we be', or when deciding how much analysis a decision deserves."
version: 1.0.0
adopted_from: don-cheli-sdd comandos/razonar (v1.0.0, Don Cheli)
tags: [reasoning, decision, commitment]
---

# Reversibility (Two-Way Doors)

Calibrate commitment by reversibility, not by size.

## Process

1. **Classify**: reversible or irreversible?
2. Reversible (two-way door): decide fast, iterate, learn from the result.
3. Irreversible (one-way door): decide slowly, analyze deeply, get owner sign-off.

## Rule

| Type     | Speed | Analysis   |
|----------|-------|------------|
| Reversible    | Fast | Minimal    |
| Irreversible  | Slow | Exhaustive |
