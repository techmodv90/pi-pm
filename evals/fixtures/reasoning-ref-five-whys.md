# 5 Whys (Root Cause)

Ask "why?" repeatedly until you reach the origin. Usually 5 levels. A root
cause is often a PROCESS problem, not just a code problem.

## Process

1. State the problem concretely.
2. Ask "Why?" → answer 1.
3. Ask "Why?" of the answer → answer 2.
4. Repeat to the root cause (usually 5 levels).
5. Propose a fix at the root cause, not at the symptom.

## Example

Problem: "Tests fail intermittently"
- Why? → Race condition
- Why? → Shared state between tests
- Why? → Global singleton
- Why? → Legacy design without review
- Why? → No architectural review process

→ Root cause: a PROCESS problem, not only code.
→ Fix: introduce architecture review + refactor the singleton.

Pairs with: first-principles, pre-mortem, root-cause-tracing methodology skill.
