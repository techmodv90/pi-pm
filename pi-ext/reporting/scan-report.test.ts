import assert from "node:assert/strict";
import test from "node:test";
import { parseCanonicalScanReportXml, prepareCanonicalScanReportArtifact, renderScanReportMarkdown } from "./scan-report.ts";

const xml = `<scan_report>
  <tech_stack><language>TypeScript &amp; Go</language><framework>SvelteKit</framework><styling>CSS</styling><database>SQLite</database><auth>Actor roles</auth><state>SQLite</state><other>Git</other></tech_stack>
  <existing_modules><module><name>Scheduler</name><description>Runs pipelines</description></module></existing_modules>
  <patterns_detected><pattern><name>Gated planning</name><location>pipeline scheduler</location></pattern></patterns_detected>
  <reusable_components><component><name>CLI helpers</name><path>core/cli-helpers.ts</path><purpose>Runs pic</purpose></component></reusable_components>
  <gaps_detected><gap><name>GAP-1</name><description>Missing evidence</description></gap></gaps_detected>
  <code_health><type_safety>Strict</type_safety><linting>Not configured</linting><tests>22 files</tests><debug_artifacts>Clean</debug_artifacts><todo_fixme>0 found</todo_fixme></code_health>
  <estimated_size><files>85</files><lines_of_code>~16,576</lines_of_code><components_modules>67</components_modules><api_routes_endpoints>4</api_routes_endpoints></estimated_size>
</scan_report>`;

test("canonical Scan XML renders deterministic owner Markdown", () => {
  const report = parseCanonicalScanReportXml(xml);
  const markdown = renderScanReportMarkdown(report);

  assert.match(markdown, /^## Scan Report/);
  assert.match(markdown, /\*\*Language:\*\* TypeScript & Go/);
  assert.match(markdown, /- \*\*Scheduler:\*\* Runs pipelines/);
  assert.match(markdown, /- \*\*CLI helpers:\*\* `core\/cli-helpers\.ts` - Runs pic/);
  assert.match(markdown, /\*\*Lines of Code:\*\* ~16,576/);
  assert.doesNotMatch(markdown, /<scan_report>/);
});

test("canonical Scan XML rejects missing required fields", () => {
  assert.throws(() => parseCanonicalScanReportXml("<scan_report><tech_stack /></scan_report>"), /missing tech_stack.language/);
});

test("canonical Scan XML rejects malformed XML", () => {
  assert.throws(() => parseCanonicalScanReportXml("<scan_report>"), /invalid XML/);
});

test("canonical Scan preparation preserves persisted XML byte-for-byte", () => {
  const prepared = prepareCanonicalScanReportArtifact(xml);
  assert.strictEqual(prepared.content, xml);
  assert.match(prepared.markdown, /^## Scan Report/);
});