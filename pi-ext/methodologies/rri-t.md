# RRI-T — Quality Verification

RRI-T is the post-build methodology for proving an integrated delivery satisfies the approved RRI requirements. It runs once per aggregate, at aggregate verification — not during the owner interview and not once per bite-sized child Task.

Select applicable perspectives and risk-relevant combinations rather than mechanically executing the full 5 x 7 x 8 matrix. Every omitted perspective, dimension, or stress axis must be recorded as N/A with a reason.

## Two-Phase Execution

RRI-T splits scenario authoring from execution and grading.

**Phase 1 — Authoring.** Applicable perspectives (personas) select risk-relevant scenarios and return a disjoint scenario list. Personas return validated scenarios only: they never execute procedures, collect evidence, or grade results, and no result, remediation, or N/A reason is authored by them. The authoring output is persisted as the `rri_t_scenarios` artifact before execution, so verification resumes from the saved scenarios without re-running persona subagents.

**Phase 2 — Execution and Grading.** The contractor executes each retained scenario's procedure in the main session against the integrated repository, records concrete executable evidence, and assigns exactly one outcome per scenario: PASS, ACCEPTABLE, PAINFUL, or FAIL with evidence, or `not_applicable` with a concrete reason when a procedure cannot execute against the integrated repository (recorded instead of failing verification). Omitted or non-executable perspectives, dimensions, and stress axes are recorded as N/A with a reason.

Every scenario is requirement-bound: it must name an approved `REQ-ID` from the aggregate's requirements.

## Scenario Identity

Each scenario carries a stable `id`. The canonical scenario identity is `dimension|stress_axis|requirement_id|id` — the id-based key used for deduplication by the Go canonical validator and the grading compiler, never the persona. Two scenarios may share persona, dimension, stress axis, and requirement while remaining distinct by id. One persisted scenario receives exactly one outcome; a duplicate deferred disposition (the same scenario recorded as `not_applicable` twice) is rejected.

## Perspectives

- **End User:** observable workflows, interaction states, accessibility, devices, recovery, and user friction.
- **Business Analyst:** business rules, policy, calculations, reporting, scope, and requirement outcomes.
- **QA / Tester:** boundaries, empty/error states, concurrency, authorization, security, compatibility, and test gates.
- **Developer:** integrated code paths, data flow, APIs, dependencies, maintainability, and technical constraints.
- **Operator:** deployment, monitoring, backup, recovery, scaling, uptime, rollback, and cost.

Run QA for every aggregate. Run End User for user-facing work, Business Analyst for behavior/rule changes, Developer for existing-code or integration changes, and Operator when production operations are in scope. A single aggregate may dispatch applicable perspectives in parallel, but each perspective receives disjoint scenario assignments and returns scenarios only; the contractor owns all execution, grading, and N/A reasons in the main session.

## Dimensions

- D1: UI/UX — visual, interaction, responsive
- D2: Business Logic — rules, calculations, workflows
- D3: Data Integrity — CRUD, validation, persistence
- D4: Integration — APIs, third parties, cross-module behavior
- D5: Performance — load time, scalability, resources
- D6: Security — authentication, authorization, sanitization, protection
- D7: Operational — deployment, monitoring, backup, recovery

## Stress Axes

TIME, DATA, ERROR, COLLABORATION, EMERGENCY, SCALE, COMPLIANCE, EVOLUTION.

## Evidence Record

Each selected scenario records:

- scenario `id`;
- perspective;
- dimension;
- stress axis;
- affected `REQ-ID`;
- procedure;
- executable evidence;
- result;
- remediation or explicit N/A reason.

## Results and Aggregate Mapping

- **PASS** — requirement is met with evidence.
- **ACCEPTABLE** — correct but measurably suboptimal; owner tradeoff is required before closure.
- **PAINFUL** — material user or operator friction; remediation or explicit owner deferral is required.
- **FAIL** — broken, unsafe, or requirement not met; aggregate verification fails.

The existing aggregate lifecycle remains authoritative:

- PASS maps to aggregate `passed`.
- ACCEPTABLE maps to `passed` only after the owner accepts the recorded tradeoff.
- PAINFUL maps to `partial` or `blocked` until fixed or explicitly deferred.
- FAIL maps to `failed`.

UI work requires D1 responsive and accessibility evidence. APIs, CLIs, libraries, data pipelines, and infrastructure record D1 as N/A with a reason.
