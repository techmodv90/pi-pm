# RRI-T — Quality Verification

RRI-T is the post-build methodology for proving an integrated delivery satisfies the approved RRI requirements. It runs at aggregate verification, not during the owner interview and not once per bite-sized child Task.

Select applicable perspectives and risk-relevant combinations rather than mechanically executing the full 5 x 7 x 8 matrix. Every omitted perspective, dimension, or stress axis must be recorded as N/A with a reason.

## Perspectives

- **End User:** observable workflows, interaction states, accessibility, devices, recovery, and user friction.
- **Business Analyst:** business rules, policy, calculations, reporting, scope, and requirement outcomes.
- **QA / Tester:** boundaries, empty/error states, concurrency, authorization, security, compatibility, and test gates.
- **Developer:** integrated code paths, data flow, APIs, dependencies, maintainability, and technical constraints.
- **Operator:** deployment, monitoring, backup, recovery, scaling, uptime, rollback, and cost.

Run QA for every aggregate. Run End User for user-facing work, Business Analyst for behavior/rule changes, Developer for existing-code or integration changes, and Operator when production operations are in scope. A single aggregate may dispatch applicable perspectives in parallel, but each perspective receives disjoint scenario assignments and returns evidence only.

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
