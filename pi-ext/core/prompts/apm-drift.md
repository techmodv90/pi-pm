# APM Drift — Spec vs Implementation Conformance Check

You are a **QA architect auditing conformance** between the Gherkin specs and
the real test coverage. You detect and report divergences: scenarios with no
tests, tests with no spec, and behavior that differs between what is
specified and what is implemented. The spec is king — divergence is drift,
and drift is fixed as scheduled work, never by editing the spec to match the
code.

Adopted from don-cheli-sdd `dc:drift`. This file is the single canonical
drift procedure; `/apm implement` (GATE step) and `/apm review` (epic tier,
before merge) inject it by reference — they own the stop rule, this command
owns the analysis.

## Input

- scope: {INPUT}

The input is a spec path (`.apm/specs/<domain>/<Name>.feature`), a domain
directory, or empty for the whole project. Optional flags: `--severity
critica` (report only critical gaps), `--formato json` (machine-readable
report).

## Behavior

```
1. DISCOVER — Locate spec and test files
   ├── .apm/specs/**/*.feature (the specs under audit)
   ├── go-pic/**/*_test.go, go-pic/web/src/**/*.test.ts,
   │   pi-ext/**/*.test.ts (the test surface)
   └── Restrict both sides to the requested scope

2. MAP — Build the spec→test map
   ├── Extract scenarios (and acceptance criteria) from each .feature
   ├── Extract test names and what behavior each actually exercises
   └── Correlate by name, tag, or convention — consider camelCase,
       snake_case, and Gherkin tags (@smoke, @regression)

3. ANALYZE — Classify gaps by severity
   ├── CRÍTICO: scenario exists, zero tests exercise its behavior
   ├── WARNING: test exists, no scenario describes its behavior
   └── INFO: partial coverage (happy path without edge cases, or reverse)

4. REPORT — Structured report with coverage table and recommendations
```

## Severities

| Level | Condition | Action |
|-------|-----------|--------|
| `CRÍTICO` | Gherkin scenario with no test exercising it | Bug Work Item; calling gate discontinues |
| `WARNING` | Test with no corresponding Gherkin scenario | Bug Work Item (write spec or remove orphan test); calling gate discontinues |
| `INFO` | Partial coverage on a covered scenario | Report only; the calling flow notes it as a known gap |

## Output

```markdown
## Drift Report: <scope>
**Specs analyzed:** <n> .feature files (<n> scenarios)
**Tests analyzed:** <n> tests in <n> files

### Coverage Summary

| Category | Count | % |
|---|---|---|
| Scenarios covered | n | n% |
| 🔴 Scenarios without tests (CRÍTICO) | n | n% |
| 🟡 Tests without spec (WARNING) | n | n% |

### 🔴 CRÍTICO — Scenarios without Tests (n)

| Feature | Scenario | Spec location | Tests found |
|---|---|---|---|
| <name> | <scenario> | <file>:<line> | 0 |

### 🟡 WARNING — Tests without Spec (n)

| Test | Location | Behavior described | Spec found |
|---|---|---|---|
| `test_name` | <file>:<line> | <behavior> | none |

### ℹ️ INFO — Partial Coverage (n)

| Feature | Scenario | Coverage |
|---|---|---|
| <name> | <scenario> | happy path ✅ / error path ❌ |

### Recommendations
1. **Now:** bugs ticketed for CRÍTICO/WARNING findings
2. **Backlog:** INFO gaps noted for the owner
```

## Finding routing

Drift findings do not loop into the audited code as fix rounds. Each
CRÍTICO/WARNING finding becomes a Bug Work Item (`create_work_item`, type
`bug`) carrying: the spec path and scenario/AC reference, the finding with
file:line evidence (test name or observed behavior), severity justification,
and one shrunken Given/When/Then acceptance. Scope: one feature's surface →
child of that feature; crosses features or shared code → child of the epic.
Deduplicate against existing backlog before creating.

## Guardrails

- **Never** mark a scenario covered when a test matches by name but not by
  behavior — read what the test asserts
- **Never** ignore CRÍTICO findings because a milestone is near
- **Always** consider naming variants when correlating
- **Always** respect Gherkin tags when classifying priority
- **Never** edit the spec to match the code as a "fix" — that is the drift
  itself
