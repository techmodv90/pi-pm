import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { buildWorkItemContinuePrompt } from "./work-item-prompts.ts";

const source = readFileSync(new URL("../agents/task-planner.md", import.meta.url), "utf8");

test("Blueprint continuation requires Contractor checkpoint before owner approval", () => {
  const source = readFileSync(new URL("./work-item-prompts.ts", import.meta.url), "utf8");
  assert.match(source, /load_blueprint_draft/);
  assert.match(source, /draft_id/);
});

test("task-planner Blueprint handoff requires Contractor checkpoint before owner approval", () => {
  assert.match(source, /save_blueprint_draft/);
  assert.match(source, /review_blueprint_checkpoint/);
  assert.match(source, /approve_blueprint_draft/);
});

test("task-planner Blueprint contract is artifact-first", () => {
  assert.match(source, /<blueprint_handoff>/);
  assert.match(source, /schema version 2/);
  assert.match(source, /load_planning_artifact/);
  assert.match(source, /save_blueprint_draft.*stage="blueprint"/s);
  assert.match(source, /temporary/);
  assert.match(source, /Do not call `save_design`/);
  assert.match(source, /Never return a Markdown planning report instead of saving the temporary draft/);
  assert.doesNotMatch(source, /\*\*Design saved:\*\*/);
  assert.doesNotMatch(source, /\*\*Task Plan nodes:\*\*/);
});

test("task-planner Task Graph contract uses XML input and JSON persistence", () => {
  assert.match(source, /<task_graph_handoff>/);
  assert.match(source, /stage="task_graph"/);
  assert.match(source, /schema version 3/);
});

// Prompt-surface isolation: the packaged contract is asserted per markdown
// section and the runtime prompts are asserted as the rendered stage-specific
// prompt, so a required Blueprint instruction cannot be satisfied by the same
// token surviving in the Task Graph surface, a stage definition, or an
// unrelated string.
function markdownSection(text: string, startHeading: string, endHeading: string): string {
  const start = text.indexOf(startHeading);
  const end = text.indexOf(endHeading);
  assert.ok(start !== -1, `missing section heading ${startHeading}`);
  assert.ok(end !== -1 && end > start, `missing section heading ${endHeading} after ${startHeading}`);
  return text.slice(start, end);
}

const blueprintContract = markdownSection(source, "## Blueprint Run", "## Task Graph Run");
const taskGraphContract = markdownSection(source, "## Task Graph Run", "## Risks And Rollback");
const runtimeBlueprintPrompt = buildWorkItemContinuePrompt({ work_item_id: "wi-prompt-spec", next_stage: "blueprint" }, { title: "Aggregate blueprint", type: "feature" });
const runtimeTaskGraphPrompt = buildWorkItemContinuePrompt({ work_item_id: "wi-prompt-spec", next_stage: "task_graph" }, { title: "Aggregate task graph", type: "feature" });
const standaloneTaskGraphPrompt = buildWorkItemContinuePrompt({ work_item_id: "wi-prompt-spec", next_stage: "task_graph" }, { title: "Standalone task graph", type: "task" });

test("Blueprint prompt surfaces carry the v2.1 solution-spec shape with marker-gated legacy behavior", () => {
  for (const [name, text] of [["task-planner Blueprint Run section", blueprintContract], ["runtime blueprint prompt", runtimeBlueprintPrompt]] as const) {
    assert.match(text, /schema_version":\s*2\.1/, `${name} names schema_version 2.1`);
    assert.match(text, /implementation_decisions/, `${name} names implementation_decisions`);
    assert.match(text, /adr_candidates/, `${name} names adr_candidates`);
    assert.match(text, /excluded_keys/, `${name} names excluded_keys`);
    // The v2.1 sections are required in shape; only markerless approved
    // artifacts keep validating under legacy rules forever.
    assert.match(text, /required in shape/, `${name} marks the v2.1 sections required`);
    assert.match(text, /implementation_decisions[\s\S]{0,120}nonempty/, `${name} requires implementation_decisions to be nonempty`);
    // Marker-gated legacy behavior: the numeric policy marker stays 2 and
    // pre-marker artifacts validate under legacy rules forever.
    assert.match(text, /decomposition_policy_version":\s*2(?!\.)/, `${name} keeps the policy marker at 2`);
    assert.match(text, /without the marker[\s\S]{0,60}under legacy rules forever/, `${name} documents marker-gated legacy behavior`);
    // The v2.1 upgrade must not reintroduce the retired v1 preview section.
    assert.match(text, /no `task_decomposition_preview`|Do not include a task_decomposition_preview/, `${name} forbids task_decomposition_preview`);
    // Scope context: exclusion authority stays RRI-owned and the binding is enforced.
    assert.match(text, /approved RRI [`]?out_of_scope[`]?/, `${name} keeps exclusion authority RRI-owned`);
    assert.match(text, /fail-closed/, `${name} enforces dangling-key rejection fail-closed`);
    assert.match(text, /no user stories and no second testing section/, `${name} forbids user stories and a second testing section`);
    // Enforced testing expectations: seams name the exact enforced command.
    assert.match(text, /exact enforced command/, `${name} requires exact enforced commands`);
    assert.match(text, /node --test reporting\/blueprint-report\.test\.ts/, `${name} names the blueprint-report test command`);
    assert.match(text, /pic work-item artifact-save blueprint/, `${name} names the artifact-save command`);
  }
});

test("ADR candidates carry eligibility criteria and are written only after owner approval", () => {
  for (const [name, text] of [["task-planner Blueprint Run section", blueprintContract], ["runtime blueprint prompt", runtimeBlueprintPrompt]] as const) {
    assert.match(text, /durable, consequential decisions/, `${name} states ADR eligibility`);
    assert.match(text, /alternatives were genuinely considered/, `${name} requires real alternatives`);
    assert.match(text, /routine implementation choices are not ADR-eligible/, `${name} excludes routine choices`);
    assert.match(text, /context"[\s\S]{0,60}choice"[\s\S]{0,60}reason"/, `${name} states the adr_candidates shape`);
    assert.match(text, /docs\/adr\//, `${name} names the ADR target directory`);
    assert.match(text, /only after[^.\n]*owner approval/, `${name} gates ADR writes on owner approval`);
    assert.match(text, /writes nothing/, `${name} forbids draft-time ADR writes`);
  }
});

test("Task Graph prompt surfaces preserve v3 output, Contract lineage, obligation coverage, and seam-bound verification", () => {
  for (const [name, text] of [["task-planner Task Graph Run section", taskGraphContract], ["runtime task_graph prompt", runtimeTaskGraphPrompt]] as const) {
    assert.match(text, /schema version 3/, `${name} requires schema version 3 output`);
    assert.match(text, /bind [`"\\]{0,2}source_contract[`"\\]{0,2} to the exact approved Contract lineage/, `${name} binds the exact Contract lineage`);
    assert.match(text, /exactly one provider[\s\S]{0,200}at least one evidence/, `${name} enforces obligation coverage`);
    assert.match(text, /seam-bound/, `${name} keeps verification seam-bound`);
    // v2.1 scope context inherited from the approved Blueprint.
    assert.match(text, /implementation_decisions[\s\S]{0,120}binding design context/, `${name} treats implementation_decisions as binding`);
    assert.match(text, /must not plan work for [`]?excluded_keys[`]? content/, `${name} excludes excluded_keys work`);
    assert.match(text, /gates its work on [`]?docs\/adr\/[`]? files being written only after the Blueprint owner approval/, `${name} gates ADR work on owner approval`);
  }
  assert.match(taskGraphContract, /Every non-deferred requirement maps to at least one node/, "packaged Task Graph Run requires requirement coverage");
  // Standalone task graphs stay on policy v1 (compatibility constraint).
  assert.match(standaloneTaskGraphPrompt, /stays on policy v1: do not set decomposition_policy_version/);
  assert.match(standaloneTaskGraphPrompt, /plain verification entry/);
  assert.doesNotMatch(standaloneTaskGraphPrompt, /source_contract/);
  assert.doesNotMatch(standaloneTaskGraphPrompt, /schema_version/);
});

test("task-planner models aggregate decomposition as vertical-slice groups and bite-sized requirement increments", () => {
  assert.match(source, /read the repository root `CONTEXT\.md`/);
  assert.match(source, /Feature normally represents one complete, demonstrable vertical slice/);
  assert.match(source, /An Epic may either contain multiple Features or represent one coherent vertical slice/);
  assert.match(source, /bite-sized increments/);
  assert.match(source, /at most two authoritative Requirements/);
  assert.match(source, /Do not target or cap the number of nodes/);
  assert.match(source, /authoritative Requirements with Given\/When\/Then/);
  assert.match(source, /vague horizontal buckets/);
  assert.match(source, /confirm granularity/);
});

test("task-planner loads codebase-design only for consequential module seams", () => {
  assert.match(source, /skills: write-plan, shape-spec, codebase-design/);
  assert.match(source, /Apply the loaded `codebase-design` skill/);
  assert.match(source, /Module Interface, Seam, Adapter/);
  assert.match(source, /Carry the chosen Seam and its invariants into the Task Graph/);
  assert.match(source, /Do not invoke its Design It Twice process unless/);
});
