# APM Constitution

> **Adopted from** [don-cheli-sdd `reglas/constitucion.md`](https://github.com/doncheli/don-cheli-sdd/blob/main/reglas/constitucion.md) (v2.0.0) — `/apm tech-plan` validates against this file.
>
> The constitution governs ALL code produced under the APM flow. Where an
> article conflicts with the canonical Work Item workflow (see AGENTS.md and
> `.apm/prd/prd-v1.0.md`), the canonical workflow wins.

---

## I. Gherkin is King (Single Source of Truth)

The `.feature` files under `.apm/specs/<domain>/` are the **ONLY** specification artifacts. No separate `spec.md` files are generated or maintained.

- All planning, implementation, and testing decisions MUST trace back to Scenarios and Rules defined in the corresponding `.feature`.
- The flow order MUST be: Read Gherkin → Generate Step Definitions (Red) → Implement Feature (Green) → Refactor.
- When a Gherkin file is ambiguous or incomplete, the gap MUST be resolved by updating the `.feature` — never by inventing requirements in later artifacts.

---

## I-B. Schema as Living Truth (DBML Lifecycle)

Schema definitions in `.apm/specs/db_schema/<domain>.dbml` follow a two-phase lifecycle:

1. **Provisional** (`@provisional` tag present): auto-generated during the Spec phase. Field names, types, and constraints are drafts.
   - Scenarios in the `.feature` MUST use the provisional field names as-is.
   - Provisional schemas MUST be reviewed and ratified before the Plan phase.

2. **Ratified** (no `@provisional` tag): reviewed during `/apm clarify` or `/apm tech-plan`. Once ratified, the DBML becomes **Absolute Truth**.
   - Any later feature in the same domain MUST extend (not replace) the ratified schema.
   - Renaming fields after ratification requires a migration note in the plan.

---

## II. Surgical Precision (Team Collaboration)

Every change MUST be the **minimum viable change** required by the current task.

- Drive-by refactoring of unrelated code, global helpers, or shared components is **PROHIBITED** unless explicitly requested.
- Formatting changes, comment additions, and import reordering outside the task's scope MUST NOT appear in diffs.

---

## III. Plug-and-Play Architecture (Modularity)

Code MUST follow the Open/Closed Principle: open for extension, closed for modification.

- New features MUST ship as modules, Service Objects, or new classes, not by inflating existing functions.
- Business logic MUST be encapsulated in Service Objects or specialized classes. Controllers, Handlers, and Routers MUST be thin (delegation only).
- Cross-cutting concerns (logging, auth, validation) MUST use middleware or decorator patterns, not inline code.

---

## IV. The "Las Vegas" Rule (Service Isolation and Mocking)

> What happens inside a service STAYS inside that service.

- Tests MUST run hermetically — no real network calls, no shared DB state, no filesystem side effects.
- Every interaction with an external service (HTTP APIs, gRPC, databases, message queues) MUST be mocked by default.
- Service classes MUST accept dependencies via dependency injection so swapping mocks for real clients is transparent.
- End-to-end tests are the ONLY exception and MUST be explicitly marked as such.

---

## IV-B. Entry Point Rule (BDD-Architecture Alignment)

Business-rule validation MUST be placed as close as possible to the entry point that the BDD `When` step invokes.

**Litmus test:** for each failure Scenario in the `.feature`, ask: *"Does the `When` step actually execute in my architecture?"* If the answer is no, the architecture violates this rule.

---

## V. Modern Code Standards

- **Type hints** are mandatory in every function signature.
- **Validation models** (Zod/Pydantic/equivalent) MUST be used for DTOs and schemas — raw dictionaries/objects are PROHIBITED for structured data.
- The language style guide (ESLint, PEP 8, etc.) MUST be followed. Where the guide conflicts with readability, readability wins.

---

## VI. Context Adaptability

Before generating code, the framework and toolchain MUST be detected by scanning configuration files (`package.json`, `go.mod`, `pyproject.toml`, etc.).

Generated code MUST NOT introduce patterns that conflict with installed dependencies or established project conventions.

---

## VII. Defensive Coding and Error Handling

- Naked `try...catch` blocks that swallow errors are PROHIBITED. Every caught exception MUST be logged with the full stack trace.
- Code MUST use custom exception classes mapping to HTTP status codes (e.g. `NotFound` → 404).
- **Stop-Loss Rule:** if a task fails (Red light) more than 3 times, work MUST stop and human guidance MUST be requested. Infinite fix-break loops are PROHIBITED.

---

## VIII. Clarification Protocol (Auto-QA)

When running `/apm clarify`, the agent acts as a **strict QA engineer** and MUST execute:

1. **Schema-Spec Consistency Check:**
   - Scan all fields in the Gherkin
   - Compare against the DBML schema
   - Error if names do not match exactly
   - Error if a `NOT NULL` field has no validation scenario

2. **Convention Check:**
   - COMMAND feature: precondition/postcondition pattern
   - QUERY feature: precondition/success pattern

3. **Auto-Generated Audit:**
   - Review auto-generated scenarios
   - Mark redundant or logically impossible ones

**Output format:** ✅ PASS / ⚠️ WARNING / ❌ FAIL

---

## Governance

- This constitution supersedes all development practices and style guides inside the repository, subject to the canonical-workflow precedence noted in the header.
- Amendments require: (1) documented justification, (2) review, (3) a migration plan for code that no longer complies.
- All PRs and code reviews MUST verify compliance with these principles.
