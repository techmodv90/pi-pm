import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { evaluateSkillFamilyMatches, listSkillFamilies, resolveSkillDirectories, validateInstructionPackSkillFamilies, validateSkillFamilies, validateTaskPlanSkillFamilies } from "./skills.ts";

function skill(root: string, relative: string, name: string, bytes = 0): string {
  const dir = join(root, relative);
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "SKILL.md"), `---\nname: ${name}\ndescription: test\n---\n${"x".repeat(bytes)}`);
  return dir;
}

function family(root: string, id: string, mandatory: string[], appliesTo?: { technologies?: string[]; fileExtensions?: string[] }): string {
  const dir = join(root, id);
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, "family.json"), JSON.stringify({ description: "test family", mandatorySkills: mandatory, ...(appliesTo ? { appliesTo } : {}) }));
  return dir;
}

test("resolveSkillDirectories keeps baselines global and merges project family overrides", () => {
  const fixture = mkdtempSync(join(tmpdir(), "task-skills-"));
  const global = join(fixture, "global");
  const project = join(fixture, "project");
  skill(global, "test-first", "test-first");
  skill(project, "test-first", "test-first");
  family(global, "languages/golang", ["golang-core"]);
  skill(global, "languages/golang/core", "golang-core");
  family(project, "languages/golang", ["golang-project"]);
  skill(project, "languages/golang/core", "golang-core");
  skill(project, "languages/golang/project", "golang-project");

  const resolved = resolveSkillDirectories({ baselineSkills: ["test-first"], skillFamilies: ["languages/golang"], globalRoot: global, projectRoot: project });
  assert.equal(resolved[0], join(global, "test-first"));
  assert.deepEqual(resolved.slice(1), [join(global, "languages/golang"), join(project, "languages/golang")]);
});

test("packaged codebase-design baseline includes its companion guidance", () => {
  const packagedRoot = fileURLToPath(new URL("../task-skills", import.meta.url));
  const resolved = resolveSkillDirectories({ baselineSkills: ["codebase-design"], skillFamilies: [], packagedRoot, globalRoot: join(tmpdir(), "missing-task-skills"), projectRoot: null });
  assert.equal(resolved.length, 1);
  assert.match(resolved[0]!, /task-skills[\\/]codebase-design$/);
});

test("resolveSkillDirectories accepts skills from the agent catalog", () => {
  const fixture = mkdtempSync(join(tmpdir(), "task-skills-"));
  const agent = join(fixture, "agent");
  skill(agent, "codanna-explore", "codanna-explore");
  const resolved = resolveSkillDirectories({ baselineSkills: ["codanna-explore"], skillFamilies: [], packagedRoot: join(fixture, "packaged"), globalRoot: agent, projectRoot: null });
  assert.deepEqual(resolved, [join(agent, "codanna-explore")]);
});

test("resolveSkillDirectories fails closed for missing, unprefixed, or oversized skills", () => {
  const fixture = mkdtempSync(join(tmpdir(), "task-skills-"));
  const global = join(fixture, "global");
  family(global, "languages/golang", ["golang-core"]);
  assert.throws(() => resolveSkillDirectories({ baselineSkills: ["missing"], skillFamilies: [], globalRoot: global }), /baseline skill.*missing/);
  skill(global, "languages/golang/core", "core");
  assert.throws(() => resolveSkillDirectories({ baselineSkills: [], skillFamilies: ["languages/golang"], globalRoot: global }), /prefix golang-/);
  writeFileSync(join(global, "languages/golang/core/SKILL.md"), "x".repeat(1024 * 1024 + 1));
  assert.throws(() => resolveSkillDirectories({ baselineSkills: [], skillFamilies: ["languages/golang"], globalRoot: global }), /1 MiB/);
});

test("skill family catalog lists qualified ids and rejects unavailable ids", () => {
  const fixture = mkdtempSync(join(tmpdir(), "task-skills-"));
  const global = join(fixture, "global");
  family(global, "languages/golang", []);

  assert.deepEqual(listSkillFamilies({ globalRoot: global, packagedRoot: join(fixture, "packaged"), projectRoot: null }), [
    { id: "languages/golang", description: "test family" },
  ]);
  assert.throws(
    () => validateSkillFamilies(["svelte"], { globalRoot: global, packagedRoot: join(fixture, "packaged"), projectRoot: null }),
    /invalid skill family id: svelte.*Available: languages\/golang/,
  );
  const options = { globalRoot: global, packagedRoot: join(fixture, "packaged"), projectRoot: null };
  assert.throws(() => validateTaskPlanSkillFamilies('```task-plan-json\n{"nodes":[{"skillFamilies":["svelte"]}]}\n```', options), /invalid skill family id: svelte/);
  assert.throws(() => validateInstructionPackSkillFamilies('{"skillFamilies":["svelte"]}', options), /invalid skill family id: svelte/);
});

test("TIP validation requires families selected by catalog metadata", () => {
  const fixture = mkdtempSync(join(tmpdir(), "task-skills-"));
  const global = join(fixture, "global");
  family(global, "frameworks/sveltekit", [], { technologies: ["sveltekit"], fileExtensions: [".svelte"] });
  const options = { globalRoot: global, packagedRoot: join(fixture, "packaged"), projectRoot: null };
  const content = '{"skillFamilies":[],"files":["apps/admin/src/routes/events/[id]"]}';
  assert.throws(
    () => validateInstructionPackSkillFamilies(content, options, [{ tech_stack_json: '{"framework":"SvelteKit"}' }]),
    /missing applicable skill families: frameworks\/sveltekit/,
  );
  assert.doesNotThrow(() => validateInstructionPackSkillFamilies(content.replace('[]', '["frameworks/sveltekit"]'), options, [{ tech_stack_json: '{"framework":"SvelteKit"}' }]));
});

test("Task Plan validation matches each node against family file-extension metadata", () => {
  const fixture = mkdtempSync(join(tmpdir(), "task-skills-"));
  const global = join(fixture, "global");
  family(global, "frameworks/sveltekit", [], { fileExtensions: [".svelte"] });
  const options = { globalRoot: global, packagedRoot: join(fixture, "packaged"), projectRoot: null };
  const plan = '```task-plan-json\n{"nodes":[{"key":"T01","files":["src/App.svelte"],"skillFamilies":[]}]}\n```';
  assert.throws(() => validateTaskPlanSkillFamilies(plan, options), /T01 missing applicable skill families: frameworks\/sveltekit/);
});

test("evaluateSkillFamilyMatches reports which appliesTo tokens fired", () => {
  const fixture = mkdtempSync(join(tmpdir(), "task-skills-"));
  const global = join(fixture, "global");
  family(global, "frameworks/sveltekit", [], { technologies: ["sveltekit", "svelte 5"], fileExtensions: [".svelte"] });
  family(global, "languages/typescript", [], { technologies: ["typescript"], fileExtensions: [".ts"] });
  const options = { globalRoot: global, packagedRoot: join(fixture, "packaged"), projectRoot: null };
  assert.deepEqual(
    evaluateSkillFamilyMatches({ files: ["src/App.svelte"], goal: "Migrate the SvelteKit store to Svelte 5 runes" }, options),
    [{ id: "frameworks/sveltekit", matchedBy: ["sveltekit", "svelte 5", ".svelte"] }],
  );
});

test("extension tokens match with a trailing boundary", () => {
  const fixture = mkdtempSync(join(tmpdir(), "task-skills-"));
  const global = join(fixture, "global");
  family(global, "languages/golang", [], { fileExtensions: [".go"] });
  const options = { globalRoot: global, packagedRoot: join(fixture, "packaged"), projectRoot: null };
  const ids = (evidence: unknown) => evaluateSkillFamilyMatches(evidence, options).map(({ id }) => id);
  assert.deepEqual(ids({ files: ["cmd/pic/main.go"] }), ["languages/golang"]);
  assert.deepEqual(ids({ files: ["cmd/pic/main.go:42"] }), ["languages/golang"]);
  assert.deepEqual(ids({ files: ["assets/logo.gone", "styles/x.gob"] }), []);
});

test("packaged family tokens route on manifests and build files", () => {
  const packagedRoot = fileURLToPath(new URL("../task-skills", import.meta.url));
  const ids = (evidence: unknown) => evaluateSkillFamilyMatches(evidence, { packagedRoot, globalRoot: join(tmpdir(), "missing-task-skills"), projectRoot: null }).map(({ id }) => id);
  assert.ok(ids({ files: ["go.mod"], goal: "harden the CLI" }).includes("languages/golang"));
  assert.ok(ids({ files: ["tsconfig.json"] }).includes("languages/typescript"));
  assert.ok(ids({ files: ["src/routes/+page.svelte"] }).includes("frameworks/sveltekit"));
});

test("packaged SvelteKit and TypeScript families resolve", () => {
  const packagedRoot = fileURLToPath(new URL("../task-skills", import.meta.url));
  const catalog = listSkillFamilies({ packagedRoot, globalRoot: join(tmpdir(), "missing-task-skills"), projectRoot: null });
  assert.ok(catalog.some(({ id }) => id === "frameworks/sveltekit"));
  assert.ok(catalog.some(({ id }) => id === "languages/typescript"));
  const resolved = resolveSkillDirectories({ baselineSkills: [], skillFamilies: ["frameworks/sveltekit", "languages/typescript"], packagedRoot, globalRoot: join(tmpdir(), "missing-task-skills"), projectRoot: null });
  assert.equal(resolved.length, 2);
});

test("packaged shadcn family resolves and routes on shadcn technology tokens only", () => {
  const packagedRoot = fileURLToPath(new URL("../task-skills", import.meta.url));
  const catalog = listSkillFamilies({ packagedRoot, globalRoot: join(tmpdir(), "missing-task-skills"), projectRoot: null });
  assert.ok(catalog.some(({ id }) => id === "frameworks/shadcn"));
  const resolved = resolveSkillDirectories({ baselineSkills: [], skillFamilies: ["frameworks/shadcn"], packagedRoot, globalRoot: join(tmpdir(), "missing-task-skills"), projectRoot: null });
  assert.equal(resolved.length, 1);
  const ids = (evidence: unknown) => evaluateSkillFamilyMatches(evidence, { packagedRoot, globalRoot: join(tmpdir(), "missing-task-skills"), projectRoot: null }).map(({ id }) => id);
  assert.ok(ids({ goal: "Add a dialog component with the shadcn-svelte CLI" }).includes("frameworks/shadcn"));
  assert.ok(!ids({ files: ["src/App.svelte"], goal: "Rework the runes store" }).includes("frameworks/shadcn"));
});