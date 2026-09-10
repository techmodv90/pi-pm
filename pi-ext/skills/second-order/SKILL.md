---
name: second-order
description: "Think through the consequences of the consequences — beyond the immediate effect. Activate when the user says: 'second order', 'unintended consequences', 'downstream effects', or when a fix or decision's side effects outweigh its first-order benefit."
version: 1.0.0
adopted_from: don-cheli-sdd comandos/razonar (v1.0.0, Don Cheli)
tags: [reasoning, perspective, consequences]
---

# Second-Order Thinking

Think the consequences of the consequences.

## Process

1. **Decision**: what are we considering?
2. **First order**: what happens immediately?
3. **Second order**: what happens as a result of that?
4. **Third order**: and after that?
5. **Evaluate**: are the long-term effects acceptable?

## Example

"Add aggressive caching for performance"
1st: ✅ 10x faster responses
2nd: ⚠️ stale data for users
3rd: ❌ users act on stale info; support gets inconsistency complaints
→ Decision: cache with short TTL + selective invalidation.
