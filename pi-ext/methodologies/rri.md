# RRI 3.0 — Context-Aware Adaptive Interview

RRI is a contractor-led requirements discovery methodology: “I see it like this — correct?” It is not an agent or orchestration runtime.

## Scope

- Epic/planning task in designed/full mode: apply all relevant personas to the whole feature.
- Quick/standard task: use a compact pass and stop when P0/P1 gaps close.
- Child or phase task: inherit parent requirements and decisions; ask only unresolved local P0/P1 deltas.

Use `rri-question-bank.md` as the canonical bank and `rri-personas.md` for the persona lenses.

## Process

1. LOAD applicable base questions by persona.
2. FILTER questions answered by Task, Scan, inherited requirements, or approved decisions; record answer, source, and confidence.
3. CONTEXTUALIZE remaining questions with project evidence and terminology.
4. ADD questions for Scan gaps, contradictions, missing contracts, and risks.
5. PRIORITIZE P0 blockers → P1 rules/security/data → P2 workflow/integration/operations → P3 polish.
6. Deduplicate overlaps while preserving persona attribution; promote evidence conflicts to P0.
7. Apply reverse-interview precedence: AUTO-ANSWERED → CHALLENGE → GUIDED → EXPLORE.
8. Apply the Owner-Question Eligibility Gate below before adding anything to the interview queue.
9. Ask the owner one question at a time unless a batch is requested.
10. Stop when P0/P1 gaps close and requirements are testable; do not ask low-value questions to hit a quota.

## Owner-Question Eligibility Gate

Ask the owner only when the answer changes observable business behavior, user experience, policy, scope, risk tolerance, cost, compliance, or operations. Explain in plain language why the decision matters and the observable consequence of each valid option.

Do not ask the owner to choose implementation details such as interfaces, structs, method signatures, query placement, transaction helpers, repository shapes, test organization, or naming. Resolve those from approved requirements, Scan evidence, existing patterns, and Design. If multiple compliant implementations remain, record the choice as AUTO-ANSWERED/Design-owned rather than an owner decision.

If only one option complies with approved requirements, use it; do not ask. Do not offer an option that violates an approved requirement merely to create a multiple-choice question. For example, whether a repository receives `AuditActor{UserID, Role}` or receives `actorID` and performs a lookup is a Design implementation detail when both preserve the approved atomic audit behavior; auditing outside the transaction is not a valid option when atomic audit is already required.

Plain-language question format: start with `Why this matters: <one observable consequence>`, then ask one complete question. Define unavoidable jargon in one short phrase. Options describe outcomes, not code shapes. Do not repeat a heading as a trailing question or use bare prompts such as `Next decision: <technical noun>`.

Only the owner approves, rejects, adjusts, defers, or escalates a proposal. Persist confirmed answers as requirements with persona, priority, source, and Gherkin-style acceptance criteria. Unresolved P0 issues block Vision/Design.
