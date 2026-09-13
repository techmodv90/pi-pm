---
name: reasoning
description: Use this skill for applying structured mental models, analytical frameworks, and systematic problem decomposition — including implicit analysis requests such as "why does this keep happening", "what should we prioritize", "what could go wrong", "what are we assuming", "is this reversible", "should we commit given uncertainty", "think backwards", "run a pre-mortem", "get to the root cause". Also trigger explicitly on "reason about", "mental model", "which model", "razonar", "analizar problema". It applies to complex problems requiring rigorous evaluation — root-cause analysis, trade-offs, prioritization under scarcity, uncertainty, failure anticipation — instead of ad-hoc intuition.
version: 1.1.0
tags: [reasoning, mental-models, decision, analysis, routing]
---

# Reasoning Router (Mental Models)

> A router, not a reasoner. It maps the problem type to the right model;
> each model's full method lives in a reference file under `references/`.
> Where this skill conflicts with the canonical Work Item workflow, the
> canonical workflow wins.

## Model Catalog

Reference paths resolve against this skill's directory.

### Analysis (understand the problem)
| # | Model | Use | Reference |
|---|-------|-----|-----------|
| 1 | First Principles | Innovation, strip assumptions | `references/first-principles.md` |
| 2 | 5 Whys | Root cause | `references/five-whys.md` |
| 3 | Map vs Territory | Validate assumptions | `references/map-vs-territory.md` |

### Decision (choose)
| # | Model | Use | Reference |
|---|-------|-----|-----------|
| 4 | Pareto | Prioritization | `references/pareto.md` |
| 5 | Opportunity Cost | Trade-offs | `references/opportunity-cost.md` |
| 6 | Reversibility | Commitment, two-way doors | `references/reversibility.md` |
| 7 | Minimize Regret | Long-term decisions | `references/minimize-regret.md` |
| 8 | Probabilistic | Uncertainty | `references/probabilistic-thinking.md` |

### Perspective (think differently)
| # | Model | Use | Reference |
|---|-------|-----|-----------|
| 9 | Inversion | Think backwards | `references/inversion.md` |
| 10 | Second-Order Effects | Consequences of consequences | `references/second-order.md` |
| 11 | Pre-Mortem | Think failure before it happens | `references/pre-mortem.md` |
| 12 | Circle of Competence | Know limits | `references/circle-of-competence.md` |

### RLM (Recursive LLM — PrimeIntellect)
| # | Model | Use | Reference |
|---|-------|-----|-----------|
| 13 | Sub-LLM Verification | Verify code against specs | `references/rlm-verification.md` |
| 14 | Chain + Context Folding | Multi-step reasoning without context rot | `references/rlm-chain-of-thought.md` |
| 15 | Recursive Decomposition | Massive inputs, large codebases | `references/rlm-recursive-decomposition.md` |

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
2. Read the model's reference file (the `Reference` path above, resolved against
   this skill's directory) and follow it — do not re-implement the model inline.
3. Mixed problems: run models in sequence (Analysis → Decision → Perspective);
   never parallel — later models need earlier output.
4. Escalation: a problem resisting two different models is a signal the
   problem is underspecified — go back to the owner with concrete questions
   instead of forcing a third model.
