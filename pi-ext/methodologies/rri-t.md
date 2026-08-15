# RRI-T — Quality Verification

RRI-T is the post-build methodology for proving the build is correct. Select risk-relevant combinations rather than mechanically executing all 280 combinations; record N/A dimensions with reasons.

## 5 Testing Personas

End User, Business Analyst, QA / Tester, Developer, Operator. Developer applies to existing codebases; Operator applies when production operations are in scope.

## 7 Testing Dimensions

- D1: UI/UX — visual, interaction, responsive
- D2: Business Logic — rules, calculations, workflows
- D3: Data Integrity — CRUD, validation, persistence
- D4: Integration — APIs, third parties, cross-module behavior
- D5: Performance — load time, scalability, resources
- D6: Security — authentication, authorization, sanitization, protection
- D7: Operational — deployment, monitoring, backup, recovery

## 8 Stress Axes

TIME, DATA, ERROR, COLLABORATION, EMERGENCY, SCALE, COMPLIANCE, EVOLUTION.

## Results

- PASS — correct and acceptance criteria met
- ACCEPTABLE — correct but measurably suboptimal
- PAINFUL — not incorrect, but causes material user or operator friction
- FAIL — broken, unsafe, or requirement not met

Map each selected scenario to persona, dimension, stress axis, REQ-ID, executable evidence, result, and remediation. Tier 1 FAIL blocks verification. PAINFUL requires remediation or explicit owner deferral. ACCEPTABLE must state the tradeoff. UI work requires D1 responsive and accessibility evidence.
