---
name: reasoning
description: "Structured mental-model router: pick the right analysis, decision, or perspective model for a problem instead of ad-hoc reasoning. Activate when the user says: 'razonar', 'reason about', 'mental model', 'which model', 'analizar problema', or when a problem needs systematic decomposition (root cause, trade-off, pre-mortem, mass context)."
version: 1.0.0
adopted_from: don-cheli-sdd habilidades/razonamiento (v1.0.0, Don Cheli; RLM models adapted from PrimeIntellect)
tags: [reasoning, mental-models, decision, analysis, routing]
---

# Reasoning Router (Mental Models)

> A router, not a reasoner. It maps the problem type to the right model;
> each model lives in its own methodology skill. Where this skill conflicts
> with the canonical Work Item workflow, the canonical workflow wins.

## Model Catalog

### Analysis (understand the problem)
| # | Model | Use | Skill |
|---|-------|-----|-------|
| 1 | First Principles | Innovation, strip assumptions | `first-principles` |
| 2 | 5 Whys | Root cause | `five-whys` |
| 3 | Map vs Territory | Validate assumptions | `map-vs-territory` |

### Decision (choose)
| # | Model | Use | Skill |
|---|-------|-----|-------|
| 4 | Pareto | Prioritization | `pareto` |
| 5 | Opportunity Cost | Trade-offs | `opportunity-cost` |
| 6 | Reversibility | Commitment, two-way doors | `reversibility` |
| 7 | Minimize Regret | Long-term decisions | `minimize-regret` |
| 8 | Probabilistic | Uncertainty | `probabilistic-thinking` |

### Perspective (think differently)
| # | Model | Use | Skill |
|---|-------|-----|-------|
| 9 | Inversion | Think backwards | `inversion` |
| 10 | Second-Order Effects | Consequences of consequences | `second-order` |
| 11 | Pre-Mortem | Think failure before it happens | `pre-mortem` |
| 12 | Circle of Competence | Know limits | `circle-of-competence` |

### RLM (Recursive LLM — PrimeIntellect)
| # | Model | Use | Skill |
|---|-------|-----|-------|
| 13 | Sub-LLM Verification | Verify code against specs | `rlm-verification` |
| 14 | Chain + Context Folding | Multi-step reasoning without context rot | `rlm-chain-of-thought` |
| 15 | Recursive Decomposition | Massive inputs, large codebases | `rlm-recursive-decomposition` |

## Decision Tree

```
What kind of problem?
├── Don't understand it → Analysis
│   ├── Hidden assumptions → First Principles
│   ├── Bug / error → 5 Whys (root-cause-tracing)
│   └── Confusing data → Map vs Territory
├── Need to choose → Decision
│   ├── What to prioritize → Pareto
│   ├── What commitment → Reversibility / Probabilistic
│   └── Long horizon → Minimize Regret
├── Need perspective → Perspective
│   ├── Avoid failure → Pre-Mortem / Inversion
│   ├── See further → Second-Order
│   └── Know enough? → Circle of Competence
└── Long context / massive input → RLM
    ├── Verify against specs → Sub-LLM Verification
    ├── Multi-step reasoning → Chain + Context Folding
    └── Big codebase / mass data → Recursive Decomposition
```

## How to Apply

1. Classify the problem via the decision tree; state the chosen model and why.
2. Load the model's skill and follow it — do not re-implement the model inline.
3. Mixed problems: run models in sequence (Analysis → Decision → Perspective);
   never parallel — later models need earlier output.
4. Escalation: a problem resisting two different models is a signal the
   problem is underspecified — go back to the owner with concrete questions
   instead of forcing a third model.
