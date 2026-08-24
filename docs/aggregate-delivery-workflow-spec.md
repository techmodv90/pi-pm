# Aggregate Delivery Workflow

This spec proposes contractor-closed executable work and one branch-owning delivery aggregate so that an Epic or Feature receives one final owner decision and one direct merge to `develop`.

## Goals

- A passed child Code Review and valid Completion Report atomically complete an executable Task without an owner decision; child Code Review is Standards/Spec review, not aggregate QA.
- Aggregate workflow status advances only after every required descendant is complete and exposes aggregate verification, owner acceptance, merge, and closure as distinct gates.
- Exactly one aggregate on a containment path may own the current delivery branch; Features own by default, Epics coordinate by default, and `integration:branch` / `integration:coordination` labels provide explicit overrides.
- Aggregate verification is the final integrated QA gate and binds the exact delivery branch head and current `develop` base.
- A branch-owning aggregate merges and pushes to `develop` after owner acceptance, then closes only after the remote target confirms the merge.
- Coordination aggregates close after aggregate verification and one owner acceptance without a Git merge.

## Non-Goals

- Automatically naming, creating, or checking out delivery branches.
- Creating pull requests or integrating with a Git hosting provider API.
- Bypassing protected-branch policy or Git credential failures.
- Giving nested aggregates independent long-lived branches beneath a branch-owning ancestor.
- Replacing the existing Worker, Reviewer, or contractor verification protocols.

## Constraints

### Compatibility

- Existing executable Completion Reports, verification reports, and pipeline lineage remain immutable and readable.
- Existing structurally aggregate Work Items with materialized children remain aggregate-compatible even if their stored type is executable.
- Current accepted executable items remain complete; no historical owner decision is rewritten.

### Performance

- Delivery state lookup uses indexed Work Item identity and containment queries; no repository-wide Git scan is introduced.
- Merge operations remain outside SQLite transactions.

### Security/Compliance

- Child agents cannot publish contractor or owner authority.
- Aggregate acceptance requires `actor_role=owner`; merge completion requires current accepted aggregate evidence.
- Git commands use argument arrays and exact recorded refs/SHAs, never shell interpolation.

### Operational

- Authorization binds the clean current branch and `develop` base; branch mode rejects authorization while checked out on `develop` or detached HEAD.
- A failed fetch, merge, push, or remote confirmation leaves durable `merge_pending`/`blocked` state and never closes the aggregate.
- Any delivery-head change after aggregate verification invalidates owner acceptance and requires fresh aggregate verification.

## Acceptance Criteria

1. Given a reviewed executable Completion Report, when contractor verification is saved as passed, then the executable becomes `done`, its dependents become eligible, and no owner decision is required.
2. Given a Feature with no branch-owning ancestor, when implementation is authorized from a non-`develop` branch, then that exact branch and current `develop` base are recorded as its delivery authority.
3. Given an Epic without an override, when implementation is authorized, then it is recorded as coordination-only and does not own a branch.
4. Given an aggregate beneath a branch-owning ancestor, when authorization attempts to assign another branch owner, then authorization fails without changing persisted authority.
5. Given all required bite-sized Tasks have focused evidence, passed child Code Review, and complete reports, when workflow status is queried, then the next stage is `aggregate_verification`; before that it remains `implement`.
6. Given passed aggregate verification, when the branch head or target base differs from the report binding, then owner acceptance fails as stale.
7. Given current aggregate verification, when the owner accepts a coordination aggregate, then it closes without a merge.
8. Given current aggregate verification, when the owner accepts a branch aggregate, then it enters `merge_pending`; only confirmed remote `develop` merge evidence moves it to `done`.
9. Given merge or push failure, when merge state is queried or retried, then the aggregate remains open with the recorded blocker and no child execution is retried.
10. Given a nested Feature under an atomic branch-owning Epic, when delivery mode resolves, then the Feature is coordination-only and the Epic remains the sole branch owner.

## Open Questions

- Should direct merges use `--no-ff` or the repository's configured merge strategy? Owner impact: commit topology and rollback behavior. Initial implementation uses `--no-ff`, matching the agreed explicit aggregate boundary.
- Should branch creation later become automatic? Owner impact: naming and checkout policy. It remains explicit until a repository-level convention is configured.
