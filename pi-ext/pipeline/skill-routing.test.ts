import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { evaluateSkillFamilyRouting, skillRoutingEventPayload } from "./skill-routing.ts";

function family(root: string, id: string, appliesTo: { technologies?: string[]; fileExtensions?: string[] }): void {
  const dir = join(root, id);
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "family.json"), JSON.stringify({ description: "test family", mandatorySkills: [], appliesTo }));
}

function fixture(): { options: { globalRoot: string; packagedRoot: string; projectRoot: null } } {
  const root = mkdtempSync(join(tmpdir(), "skill-routing-"));
  const global = join(root, "global");
  family(global, "languages/typescript", { technologies: ["typescript"], fileExtensions: [".ts"] });
  family(global, "frameworks/sveltekit", { technologies: ["sveltekit"] });
  return { options: { globalRoot: global, packagedRoot: join(root, "packaged"), projectRoot: null } };
}

test("evaluateSkillFamilyRouting reports selected, matched, and missing families without throwing", () => {
  const { options } = fixture();
  const pack = { id: "wip-1", content_json: JSON.stringify({ skillFamilies: ["languages/typescript"], goal: "Migrate the store to SvelteKit runes" }) };
  const evaluation = evaluateSkillFamilyRouting(pack, [], options);
  assert.deepEqual(evaluation.selectedFamilies, ["languages/typescript"]);
  // The pack content names its own selected families, so they self-match; the
  // routing signal that matters is matched-minus-selected (missingFamilies).
  assert.deepEqual(evaluation.matchedFamilies.map(({ id }) => id), ["frameworks/sveltekit", "languages/typescript"]);
  assert.deepEqual(evaluation.missingFamilies, ["frameworks/sveltekit"]);
  assert.deepEqual(evaluation.evidenceSources, ["pack_content"]);
});

test("malformed pack content falls back to the persisted skill_families_json column", () => {
  const { options } = fixture();
  const pack = { id: "wip-1", content_json: "not json", skill_families_json: '["languages/typescript"]' };
  const evaluation = evaluateSkillFamilyRouting(pack, [{ tech_stack_json: '{"framework":"SvelteKit"}' }], options);
  assert.deepEqual(evaluation.selectedFamilies, ["languages/typescript"]);
  assert.deepEqual(evaluation.matchedFamilies.map(({ id }) => id), ["frameworks/sveltekit"]);
  assert.deepEqual(evaluation.missingFamilies, ["frameworks/sveltekit"]);
  assert.deepEqual(evaluation.evidenceSources, ["scan_artifact"]);
});

test("packs without evidence sources produce an empty evaluation", () => {
  const { options } = fixture();
  const evaluation = evaluateSkillFamilyRouting({ id: "wip-1", content_json: "not json" }, [], options);
  assert.deepEqual(evaluation.selectedFamilies, []);
  assert.deepEqual(evaluation.matchedFamilies, []);
  assert.deepEqual(evaluation.missingFamilies, []);
  assert.deepEqual(evaluation.evidenceSources, []);
});

test("routing payload uses snake_case keys for the work_item_events wide event", () => {
  const evaluation = {
    selectedFamilies: ["languages/typescript"],
    matchedFamilies: [{ id: "languages/typescript", matchedBy: [".ts"] }],
    missingFamilies: [],
    evidenceSources: ["pack_content"],
  };
  assert.deepEqual(skillRoutingEventPayload("worker", "wip-1", evaluation), {
    stage: "worker",
    pack_id: "wip-1",
    selected_families: ["languages/typescript"],
    matched_families: [{ id: "languages/typescript", matched_by: [".ts"] }],
    missing_families: [],
    evidence_sources: ["pack_content"],
  });
});
