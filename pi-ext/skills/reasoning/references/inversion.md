# Inversion

Solve the problem backwards: how would we GUARANTEE failure?

## Process

1. **Define** the desired objective.
2. **Invert**: how would we guarantee failure?
3. **List** all the ways to fail.
4. **Invert** each failure → a preventive action.
5. **Prioritize** the preventive actions.

## Example

Objective: "Successful v2.0 launch"
Ways to guarantee failure: no regression tests; deploy Friday 6pm; no rollback plan; ignore beta feedback.
→ Inverted plan: mandatory regression tests; deploy Tuesday morning; documented rollback; beta-testing cycle.

Pairs with: pre-mortem, first-principles.
