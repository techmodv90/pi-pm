# RRI Persona Analysis

Apply each relevant perspective within the contractor's RRI pass. Personas are analysis lenses, not separate agents or stakeholders.

Use `rri-question-bank.md` as the canonical bank. For each persona:

1. LOAD its base questions.
2. FILTER known answers into AUTO-ANSWERED with source and confidence.
3. CONTEXTUALIZE unanswered questions.
4. ADD Scan-gap and risk questions.
5. PRIORITIZE P0 → P1 → P2 → P3.

Use AUTO-ANSWERED when evidence establishes the answer, CHALLENGE-PROPOSED in CHALLENGE mode when evidence supports a likely decision, SMART-ASKED in GUIDED mode for bounded choices, and SMART-ASKED in EXPLORE mode only for genuine unknowns.

## Lenses

- **End User:** identity, proficiency, environment, goals, workflow, pain points, devices, accessibility, localization.
- **Business Analyst:** goals, metrics, rules, entities, reporting, compliance, constraints, stakeholders, notifications, imports/exports.
- **QA / Tester:** boundaries, empty/error states, volume/concurrency, recovery, security, authorization, performance, compatibility, test gates.
- **Developer:** existing patterns, debt, performance, dependencies, type/lint/test conventions, integrations, local development, CI. N/A for a genuinely new codebase.
- **Operator:** deployment, environments, observability, backup/recovery, rollback, scaling, cost, geography. N/A when production operations are out of scope.

Record candidate questions with persona, priority, classification, mode, suggested answers, reason, and requirement area. Record skipped topics with reasons.
