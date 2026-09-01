import { XMLParser, XMLValidator } from "fast-xml-parser";

export const RRI_T_PERSONAS = ["End User", "Business Analyst", "QA / Tester", "Developer", "Operator"] as const;
// RRI-T authoring fields: the only fields a persona scenario may carry. Grading
// records (evidence/result/remediation) belong to the later contractor phase.
export const RRI_T_AUTHORING_FIELDS = ["id", "dimension", "stress_axis", "requirement_id", "procedure", "remediation_hint"] as const;
export const RRI_T_DIMENSIONS = new Set(["D1", "D2", "D3", "D4", "D5", "D6", "D7"]);
export const RRI_T_STRESS_AXES = new Set(["TIME", "DATA", "ERROR", "COLLABORATION", "EMERGENCY", "SCALE", "COMPLIANCE", "EVOLUTION"]);

export function normalizeRriTXml(output: string): string {
  const trimmed = output.trim().replace(/^```(?:xml)?\s*([\s\S]*?)\s*```$/, "$1").trim();
  const start = trimmed.indexOf("<rri_t_persona");
  const end = trimmed.lastIndexOf("</rri_t_persona>");
  if (start < 0 || end <= start) throw new Error("RRI-T persona output must contain one rri_t_persona document");
  return trimmed.slice(start, end + "</rri_t_persona>".length);
}

// RRI-T authoring boundary: LLM persona output routinely carries bare `&` in
// scenario text (commands like `a && b`, prose like `R&D`), which is invalid XML
// and fails the validator. Repair bare ampersands to `&amp;` before validation;
// well-formed entities pass through untouched and structural malformation still
// fails closed.
export function repairBareAmpersands(xml: string): string {
  return xml.replace(/&(?!(?:amp|lt|gt|quot|apos|#\d+|#x[0-9a-fA-F]+);)/g, "&amp;");
}

// RRI-T authoring boundary: a concrete procedure must pair a non-empty command
// with a non-empty expected observable on opposite sides of the supported
// `→`/`->` delimiter. An arrow alone or a blank side is not a concrete procedure.
export function hasConcreteProcedure(procedure: string): boolean {
  const delimiter = procedure.match(/(?:→|->)/);
  if (!delimiter || delimiter.index === undefined) return false;
  const command = procedure.slice(0, delimiter.index).trim();
  const expected = procedure.slice(delimiter.index + delimiter[0].length).trim();
  return command.length > 0 && expected.length > 0;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function parseRriTPersonaResult(output: string, expectedPersona: string): any {
  const xml = repairBareAmpersands(normalizeRriTXml(output));
  // RRI-T authoring boundary: reject malformed XML before any per-scenario work so
  // a broken document fails closed instead of being leniently parsed.
  const validation = XMLValidator.validate(xml);
  if (validation !== true) throw new Error(`RRI-T persona ${expectedPersona} returned invalid XML: ${validation.err?.msg || "malformed document"}`);
  const parsed = new XMLParser({ ignoreAttributes: false, attributeNamePrefix: "@_", trimValues: true }).parse(xml)?.rri_t_persona;
  if (!parsed || parsed["@_persona"] !== expectedPersona) throw new Error(`RRI-T output has unexpected persona; expected ${expectedPersona}`);
  const scenarios = Array.isArray(parsed.scenarios?.scenario) ? parsed.scenarios.scenario : parsed.scenarios?.scenario ? [parsed.scenarios.scenario] : [];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const normalized = scenarios.map((scenario: any) => {
    const value = Object.fromEntries(["id", "dimension", "stress_axis", "requirement_id", "procedure", "remediation_hint", "evidence", "result", "remediation"].map((key) => [key, String(scenario[key] || "").trim()]));
    // RRI-T authoring boundary: personas author the six scenario fields only;
    // evidence/result are contractor-phase execution records, so they are not
    // required and are ignored when present. A concrete procedure must pair a
    // command with its expected observable (`command → expected`).
    if (!value.id || !RRI_T_DIMENSIONS.has(value.dimension) || !RRI_T_STRESS_AXES.has(value.stress_axis) || !value.requirement_id || !value.procedure || !hasConcreteProcedure(value.procedure) || !value.remediation_hint) throw new Error(`RRI-T ${expectedPersona} returned an invalid scenario`);
    return value;
  });
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const notApplicable = parsed.not_applicable?.topic ? (Array.isArray(parsed.not_applicable.topic) ? parsed.not_applicable.topic : [parsed.not_applicable.topic]).map((topic: any) => ({ topic: String(topic.topic || ""), reason: String(topic.reason || "") })) : [];
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  if (notApplicable.some((topic: any) => !topic.topic || !topic.reason)) throw new Error(`RRI-T ${expectedPersona} returned an invalid N/A topic`);
  return { persona: expectedPersona, scenarios: normalized, not_applicable: notApplicable, open_blockers: parsed.open_blockers?.blocker ? (Array.isArray(parsed.open_blockers.blocker) ? parsed.open_blockers.blocker : [parsed.open_blockers.blocker]).map(String) : [] };
}

// Planning profile constraint: dispatch authority for planning stages comes from
// the persisted Plan profile (work_item_profiles), falling back to deterministic
// kind/depth resolution only when no profile has been persisted yet (e.g. before

// RRI-T authoring merge (OB-3): the duplicate scenario identity is the
// requirement-bound authoring key (dimension|stress_axis|requirement_id|id) —
// never the authoring persona — so the same scenario authored by several personas
// collapses to one deterministic row that preserves the first author's persona
// metadata. Only the six authoring fields survive; grading records are stripped.
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function mergeRriTAuthoringResults(results: Array<{ persona: string; scenarios: any[]; not_applicable: any[]; open_blockers: any[] }>, personas: readonly string[]): {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  methodology: "rri-t"; personas: string[]; scenarios: any[]; not_applicable: any[]; open_blockers: any[];
} {
  const seen = new Set<string>();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const scenarios: any[] = [];
  for (const result of results) {
    for (const scenario of result.scenarios || []) {
      const key = `${scenario.dimension}|${scenario.stress_axis}|${scenario.requirement_id}|${scenario.id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      scenarios.push({ ...Object.fromEntries(RRI_T_AUTHORING_FIELDS.map((field) => [field, scenario[field]])), persona: result.persona });
    }
  }
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const notApplicable = results.flatMap((result) => (result.not_applicable || []).map((topic: any) => ({ ...topic, persona: result.persona })));
  const openBlockers = results.flatMap((result) => (result.open_blockers || []));
  return { methodology: "rri-t", personas: [...personas], scenarios, not_applicable: notApplicable, open_blockers: openBlockers };
}
