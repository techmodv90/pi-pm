---
name: domain-modeling
description: Build and sharpen a project's domain model. Use when discussing codebase terminology, writing or editing a CONTEXT.md, or recording or editing an ADR. Part of the APM workspace — decisions and glossary live under .apm/.
---

# Domain Modeling

Actively build and sharpen the project's domain model as you design. This is the *active* discipline: challenging terms, inventing edge-case scenarios, and writing the glossary and decisions down the moment they crystallise. (Merely *reading* `CONTEXT.md` for vocabulary is not this skill: that's a one-line habit any skill can do. This skill is for when you're changing the model, not just consuming it.)

## File structure

Artifacts live in the APM workspace (`.apm/`), alongside specs, PRD, and design docs. Root `docs/` is for product documentation only (guides, ledgers) — never decisions or terminology.

Single-context repo (most repos):

```
/
├── docs/                              ← product documentation only
└── .apm/
    ├── CONTEXT.md                     ← glossary
    └── adr/
        ├── 0001-lean-work-item-flow.md
        └── 0002-agent-tool-dispatch-seam.md
```

Multi-context repo (presence of `.apm/CONTEXT-MAP.md`):

```
/
├── docs/                              ← product documentation only
└── .apm/
    ├── CONTEXT-MAP.md                 ← points to each context's glossary
    ├── adr/                           ← ALL decisions, one sequence
    │   ├── 0001-shared-decision.md
    │   └── 0002-ordering-specific.md  ← Context: ordering
    ├── CONTEXT.md                     ← root context glossary
    └── contexts/
        ├── ordering/CONTEXT.md
        └── billing/CONTEXT.md
```

Glossaries may be colocated next to the code they describe instead of under `.apm/contexts/` — the map points wherever they live. ADRs never fork into per-context directories: one global `NNNN` sequence in `.apm/adr/`, with an optional `Context:` line naming the owning context. Forked numbering makes every bare citation ("ADR-0007") ambiguous and turns cross-context moves into renumbering.

Create files lazily: only when you have something to write. If no `CONTEXT.md` exists, create one when the first term is resolved. If no `.apm/adr/` exists, create it when the first ADR is needed.

## During the session

### Challenge against the glossary

When the user uses a term that conflicts with the existing language in `CONTEXT.md`, call it out immediately. "Your glossary defines 'cancellation' as X, but you seem to mean Y. Which is it?"

### Sharpen fuzzy language

When the user uses vague or overloaded terms, propose a precise canonical term. "You're saying 'account': do you mean the Customer or the User? Those are different things."

### Discuss concrete scenarios

When domain relationships are being discussed, stress-test them with specific scenarios. Invent scenarios that probe edge cases and force the user to be precise about the boundaries between concepts.

### Cross-reference with code

When the user states how something works, check whether the code agrees. If you find a contradiction, surface it: "Your code cancels entire Orders, but you just said partial cancellation is possible. Which is right?"

### Update CONTEXT.md inline

When a term is resolved, update `CONTEXT.md` right there. Don't batch these up: capture them as they happen. Use the format in [CONTEXT-FORMAT.md](./CONTEXT-FORMAT.md).

`CONTEXT.md` should be totally devoid of implementation details. Do not treat `CONTEXT.md` as a spec, a scratch pad, or a repository for implementation decisions. It is a glossary and nothing else.

### Offer ADRs sparingly

Only offer to create an ADR when all three are true:

1. **Hard to reverse**: the cost of changing your mind later is meaningful
2. **Surprising without context**: a future reader will wonder "why did they do it this way?"
3. **The result of a real trade-off**: there were genuine alternatives and you picked one for specific reasons

If any of the three is missing, skip the ADR. Use the format in [ADR-FORMAT.md](./ADR-FORMAT.md).

## Relation to APM commands

- `/apm design` produces the heavyweight feature design doc (`.apm/design/`); its durable decisions cite `.apm/adr/NNNN` entries rather than duplicating them.
- Spec quality checks (`/apm spec-score`, `/apm spec-validate`) read `CONTEXT.md` for canonical terminology — undefined domain terms in specs should drive glossary entries.
