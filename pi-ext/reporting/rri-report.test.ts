import assert from "node:assert/strict";
import test from "node:test";
import { parseRriReportJson, renderRriReportMarkdown } from "./rri-report.ts";

const legacyReport = {
  project_name: "Legacy Project", generated: "2026-08-17",
  requirements_matrix: [{ req_id: "REQ-001", requirement: "Clean baseline", source: "RRI Q#1", priority: "P0", persona: "Developer" }],
  auto_answered: [],
  decisions_log: [{ decision: "Release baseline", options_considered: "Clean vs dirty", chosen: "Clean", rationale: "Reproducibility" }],
  open_questions: [{ id: "Q-LEGACY", question: "Legacy open question" }],
};

const resolvedFrontierRow = {
  id: "Q-1", question: "Which ship order applies?",
  status: "resolved", priority: "P1", mode: "hitl", blocks: true,
  resolution: { answer: "CLI first", source: "Owner confirm" },
};

const openFrontierRow = {
  id: "Q-2", question: "Which export formats ship first?",
  status: "open", priority: "P0", mode: "afk", blocks: false,
};

const markedReport = (openQuestions: unknown[]) => ({
  project_name: "Marked Project", generated: "2026-09-01", rri_policy_version: 2,
  requirements_matrix: [{ req_id: "REQ-F1-1", requirement: "Frontier schema", source: "RRI Q#2", priority: "P1", persona: "Business Analyst" }],
  auto_answered: [{ topic: "Storage", details: "SQLite already persisted", resolution: "Keep SQLite" }],
  decisions_log: [{ decision: "Schema gating", options_considered: "Global vs marker-gated", chosen: "Marker-gated", rationale: "Legacy tolerance" }],
  not_yet_specified: [{ uncertainty: "Export formats", graduation_path: "Resolve with the owner before contracts" }],
  out_of_scope: [{ exclusion: "Cloud sync", reason: "Outside the epic scope" }],
  open_questions: openQuestions,
});

test("legacy RRI report without the policy marker validates and renders unchanged", () => {
  const report = parseRriReportJson(JSON.stringify(legacyReport));
  const markdown = renderRriReportMarkdown(report);
  assert.match(markdown, /^# RRI REPORT: Legacy Project/);
  assert.match(markdown, /REQ-001.*Clean baseline/);
  assert.match(markdown, /\| Release baseline \| Clean vs dirty \| Clean \| Reproducibility \|/);
  assert.match(markdown, /- \*\*Q-LEGACY:\*\* Legacy open question/);
  assert.doesNotMatch(markdown, /status:/);
});

test("marked RRI report with frontier open_questions rows parses and renders the resolution", () => {
  const report = parseRriReportJson(JSON.stringify(markedReport([resolvedFrontierRow, openFrontierRow])));
  const markdown = renderRriReportMarkdown(report);
  assert.match(markdown, /^# RRI REPORT: Marked Project/);
  assert.match(markdown, /- \*\*Q-2:\*\* .* \(status: open, priority: P0, mode: afk, blocks: false\)/);
  assert.match(markdown, /- \*\*Q-1:\*\* .* \(status: resolved, priority: P1, mode: hitl, blocks: true, resolution: CLI first \(source: Owner confirm\)\)/);
});

test("marked RRI report rejects rows missing required frontier fields", () => {
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ id: "Q-1", question: "No status", priority: "P1", mode: "hitl", blocks: true }]))),
    /row Q-1 requires status/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ id: "Q-1", question: "No priority", status: "open", mode: "hitl", blocks: true }]))),
    /row Q-1 requires priority/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ id: "Q-1", question: "No mode", status: "open", priority: "P1", blocks: true }]))),
    /row Q-1 requires mode/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ id: "Q-1", question: "No blocks", status: "open", priority: "P1", mode: "hitl" }]))),
    /row Q-1 requires blocks/,
  );
});

test("marked RRI report rejects invalid enum values and unresolved resolved rows", () => {
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...openFrontierRow, status: "parked" }]))),
    /row Q-2 has invalid status parked/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...openFrontierRow, priority: "P9" }]))),
    /row Q-2 has invalid priority P9/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...openFrontierRow, mode: "async" }]))),
    /row Q-2 has invalid mode async/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...resolvedFrontierRow, resolution: undefined }]))),
    /row Q-1 requires resolution with answer and source when status is resolved or deferred/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...resolvedFrontierRow, resolution: { answer: "Later" } }]))),
    /row Q-1 requires resolution answer and source to be non-empty strings/,
  );
});

test("open marked rows reject malformed resolution values like Go unmarshalling", () => {
  // A present resolution must be well-typed regardless of row status: Go rejects
  // the JSON at unmarshal time and the renderer would crash on non-string fields.
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...openFrontierRow, resolution: { answer: 1, source: "Owner confirm" } }]))),
    /row Q-2 requires resolution answer and source to be non-empty strings/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...openFrontierRow, resolution: { answer: "Later", source: "" } }]))),
    /row Q-2 requires resolution answer and source to be non-empty strings/,
  );
  // Falsy scalars must not slip past a truthiness guard: Go unmarshalling
  // rejects them because they cannot decode into the rriResolution struct.
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...openFrontierRow, resolution: "" }]))),
    /row Q-2 requires resolution answer and source to be non-empty strings/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...openFrontierRow, resolution: 0 }]))),
    /row Q-2 requires resolution answer and source to be non-empty strings/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...openFrontierRow, resolution: false }]))),
    /row Q-2 requires resolution answer and source to be non-empty strings/,
  );
  // null behaves like Go pointer unmarshalling: treated as absent, so a null
  // resolution on a resolved row fails the requires-resolution check instead.
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...resolvedFrontierRow, resolution: null }]))),
    /row Q-1 requires resolution with answer and source when status is resolved or deferred/,
  );
});

test("marked RRI report rejects malformed policy marker types like Go unmarshalling", () => {
  assert.throws(
    () => parseRriReportJson(JSON.stringify({ ...markedReport([]), rri_policy_version: "2" })),
    /RRI rri_policy_version must be an integer/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify({ ...markedReport([]), rri_policy_version: true })),
    /RRI rri_policy_version must be an integer/,
  );
  // Go int unmarshalling rejects fractional markers; TS must not accept 1.5 as legacy.
  assert.throws(
    () => parseRriReportJson(JSON.stringify({ ...markedReport([]), rri_policy_version: 1.5 })),
    /RRI rri_policy_version must be an integer/,
  );
});

test("null rri_policy_version stays legacy like Go int unmarshalling", () => {
  const report = parseRriReportJson(JSON.stringify({ ...legacyReport, rri_policy_version: null }));
  const markdown = renderRriReportMarkdown(report);
  assert.match(markdown, /^# RRI REPORT: Legacy Project/);
  assert.doesNotMatch(markdown, /status:/);
});

test("open_questions resolution values must be strings like Go unmarshalling", () => {
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...resolvedFrontierRow, resolution: { answer: 1, source: "Owner confirm" } }]))),
    /row Q-1 requires resolution answer and source to be non-empty strings/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify(markedReport([{ ...resolvedFrontierRow, resolution: { answer: "CLI first", source: true } }]))),
    /row Q-1 requires resolution answer and source to be non-empty strings/,
  );
});

test("marked RRI report renders both scope sections and never a Destination", () => {
  const markdown = renderRriReportMarkdown(parseRriReportJson(JSON.stringify(markedReport([]))));
  assert.match(markdown, /## NOT YET SPECIFIED\n- \*\*Export formats:\*\* graduation path -> Resolve with the owner before contracts/);
  assert.match(markdown, /## OUT OF SCOPE\n- \*\*Cloud sync:\*\* Outside the epic scope/);
  assert.doesNotMatch(markdown, /destination/i);
  // A destination key in the payload is ignored: no Destination field exists in
  // the schema because Work Item goals remain the destination authority.
  const withDestination = renderRriReportMarkdown(parseRriReportJson(JSON.stringify({ ...markedReport([]), destination: "Work Item goals" })));
  assert.doesNotMatch(withDestination, /destination/i);
});

test("marked RRI report rejects a missing or malformed scope section", () => {
  const withoutUncertainty: Record<string, unknown> = markedReport([]);
  delete withoutUncertainty.not_yet_specified;
  assert.throws(
    () => parseRriReportJson(JSON.stringify(withoutUncertainty)),
    /requires the not_yet_specified section/,
  );
  const withoutOutOfScope: Record<string, unknown> = markedReport([]);
  delete withoutOutOfScope.out_of_scope;
  assert.throws(
    () => parseRriReportJson(JSON.stringify(withoutOutOfScope)),
    /requires the out_of_scope section/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify({ ...markedReport([]), not_yet_specified: [{ uncertainty: "Export formats" }] })),
    /not_yet_specified rows require uncertainty and graduation_path/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify({ ...markedReport([]), out_of_scope: [{ exclusion: "Cloud sync" }] })),
    /out_of_scope rows require exclusion and reason/,
  );
});

test("legacy RRI report stays valid without scope sections", () => {
  const report = parseRriReportJson(JSON.stringify(legacyReport));
  const markdown = renderRriReportMarkdown(report);
  assert.match(markdown, /## NOT YET SPECIFIED\n- None/);
  assert.match(markdown, /## OUT OF SCOPE\n- None/);
});

test("marked RRI report rejects unsupported policy versions and keeps legacy tolerance", () => {
  assert.throws(
    () => parseRriReportJson(JSON.stringify({ ...markedReport([]), rri_policy_version: 3 })),
    /RRI rri_policy_version 3 is unsupported/,
  );
  assert.throws(
    () => parseRriReportJson(JSON.stringify({ project_name: "Project", generated: "2026-08-17", requirements_matrix: [{ req_id: "REQ-001" }], auto_answered: [], decisions_log: [], open_questions: [] })),
    /incomplete row/,
  );
});
