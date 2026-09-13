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
