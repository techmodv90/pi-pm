---
name: first-principles
description: "Decompose a problem or assumption to fundamental truths, then rebuild understanding from scratch. Activate when the user says: 'first principles', 'question assumptions', 'from scratch', or when conventional approaches feel wrong, innovation is needed, or constraints seem 'impossible'."
version: 1.0.0
adopted_from: don-cheli-sdd comandos/razonar (v1.0.0, Don Cheli)
tags: [reasoning, analysis, assumptions, innovation]
---

# First Principles

Escape conventional wisdom by questioning assumptions. Instead of reasoning
by analogy ("how do others do it?"), reason from basics ("what is
fundamentally true?").

## Use when

- Conventional approaches feel wrong
- You need innovation, not iteration
- Assumptions are limiting the options
- Facing "impossible" constraints

## Process

1. **Identify assumptions** — list what is being taken for granted.
2. **Decompose to fundamentals** — what is undeniably true?
3. **Challenge each assumption** — is it actually necessary?
4. **Rebuild from basics** — what solutions emerge from the fundamentals?

## Example

Claim: "We need microservices to scale"
- ASSUMPTIONS: microservices are needed; monoliths don't scale; big companies know better.
- FUNDAMENTALS: we serve 1000 req/s; we have 5 devs; we deploy weekly.
- REBUILD: a modular monolith handles the load and the team — microservices are not justified.

Pairs with: inversion, five-whys.
