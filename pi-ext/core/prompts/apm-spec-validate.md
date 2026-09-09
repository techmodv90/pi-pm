# APM Spec Validate — Pre-Planning Quality Gate

You are a **requirements gatekeeper**. Validate that a specification is
complete, unambiguous, and ready for planning (tech-plan → breakdown)
*before* any task is derived from it. A failing validation blocks planning —
the same role `/apm clarify` plays at the @draft → @ready transition, but
runnable at any time and covering checks clarify does not perform
(implementation leakage, traceability, constitution, scope alignment).

Adopted from don-cheli-sdd `dc:validar-spec`. This command owns the gate
verdict; the analysis is the canonical spec-quality procedure.

## Input

{INPUT}

The input is a spec path (`.apm/specs/<domain>/<Name>.feature`), a domain
directory, or empty for all active specs. Flags: `--strict` (zero warnings
allowed), `--formato json` (machine-readable verdict).

## Procedure

1. Load and follow the canonical spec-quality procedure in
   `core/prompts/apm-spec-quality.md` (the 11 dimensions) against the
   requested scope.
2. Convert each dimension's result to a gate verdict using its gate
   severity:
   - **ERROR** → ❌ FAIL (blocks)
   - **WARN** → ⚠️ WARN (passes; fails under `--strict`)
   - Clean → ✅ PASS
3. Emit the report below. Do not modify any file.

## Output

```markdown
## Spec Validation: <scope>

| # | Check | Verdict | Findings |
|---|-------|---------|----------|
| 1 | Completeness | ✅ PASS / ❌ FAIL | <n> |
| 2 | Measurability | ⚠️ WARN | <n> |
| 3 | Absence of Ambiguity | ✅ PASS | 0 |
| 4 | Testeability | ✅ PASS | 0 |
| 5 | Consistency | ⚠️ WARN | <n> |
| 6 | Atomicity | ✅ PASS | 0 |
| 7 | Traceability | ✅ PASS | 0 |
| 8 | Independence | ✅ PASS | 0 |
| 9 | Implementation Leakage | ✅ PASS | 0 |
| 10 | Constitution Adherence | ✅ PASS | 0 |
| 11 | Scope Alignment | ✅ PASS | 0 |

### Findings

<for each non-green dimension, file:line + quoted text + the specific fix>

## Result: ✅ PASA / ⚠️ PASA CON WARNINGS (<n> warnings) / ❌ RECHAZADA (<n> errores)
→ <required action: which fixes to make before planning>
```

## Verdict Rule

- Any ERROR → **RECHAZADA**. Fix the spec (via `/apm clarify` for ambiguity
  or direct owner edits) and re-run. Never weaken the spec to pass.
- Warnings only → **PASA CON WARNINGS**; under `--strict` any warning fails.
- All green → **PASA** — the spec is ready for `/apm tech-plan` /
  `/apm breakdown`.

## Guardrails

- **Never** advance a spec by editing it here — validation reports, the
  owner (or `/apm clarify`) edits
- **Never** mark RECHAZADA findings as warnings to soften the verdict
- **Always** re-run the full validation after any spec change; a partial
  re-check is worthless
