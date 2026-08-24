# Design It Twice

Use this process only when the user wants alternatives for a chosen Interface or when a durable, high-impact Seam has multiple credible designs.

## 1. Frame The Problem

Present:

- constraints every Interface must satisfy;
- dependencies and their categories from `DEEPENING.md`;
- a small illustrative sketch that grounds the constraints without proposing the answer.

## 2. Explore Radically Different Interfaces

Use at least three parallel read-only design agents when the runtime supports isolated parallel exploration. Give each the same evidence and vocabulary from `SKILL.md` and the repository `CONTEXT.md`, but a different design constraint:

1. Minimize the Interface to one to three entry points and maximize leverage.
2. Maximize flexibility across known use cases without speculative extension points.
3. Optimize the common caller so the default case is trivial.
4. When applicable, design around justified ports and Adapters for cross-Seam dependencies.

Each design must provide:

1. Interface, including types, invariants, ordering, and errors.
2. Caller example.
3. Behavior hidden in the Implementation.
4. Dependency and Adapter strategy.
5. Trade-offs in depth, leverage, locality, and Seam placement.

## 3. Compare And Recommend

Present each design clearly, then compare them by depth, locality, testability, and Seam placement. Recommend one design or a specific hybrid. Do not present an unranked menu.
