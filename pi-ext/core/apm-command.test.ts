import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { parseSpecArgs } from "./apm-spec.ts";

function readSource(path: string): string {
  return readFileSync(fileURLToPath(new URL(path, import.meta.url)), "utf8");
}

test("hides the full PRD prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-prd.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-prd", prompt\)/);
  const shared = readSource("./apm-shared.ts");
  assert.match(shared, /display: false/);
  assert.match(shared, /triggerTurn: true/);
});

test("hides the full spec prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-spec.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-spec", prompt\)/);
});

test("hides the full clarify prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-clarify.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-clarify", prompt\)/);
});

test("hides the full tech-plan prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-tech-plan.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-tech-plan", prompt\)/);
});

test("hides the full breakdown prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-breakdown.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-breakdown", prompt\)/);
});

test("start prompt routes levels conservatively and never pre-executes gates", () => {
  const source = readSource("./apm-start.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-start", prompt\)/);
  const prompt = readSource("./prompts/apm-start.md");
  assert.match(prompt, /Level = max\(scores\)/);
  assert.match(prompt, /PoC detection/);
  assert.match(prompt, /never pre-execute gate steps/);
  // Micro pipeline satisfies the real /apm implement gate (approved .tasks.md).
  assert.match(prompt, /single-task \.tasks\.md/);
});

test("hides the full archive prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-archive.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-archive", prompt\)/);
  // Fail-closed on verification: the archivist never self-tags @implemented.
  const prompt = readSource("./prompts/apm-archive.md");
  assert.match(prompt, /@implemented/);
  assert.match(prompt, /Never add `@implemented` yourself/);
  assert.match(prompt, /\.apm\/specs\/archive\/<domain>\/<Feature>\//);
});

test("hides the full distill prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-distill.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-distill", prompt\)/);
});

test("hides the full implement prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-implement.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-implement", prompt\)/);
});

test("hides the full drift prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-drift.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-drift", prompt\)/);
});

test("hides the full spec-validate and spec-score prompts while retaining them for the LLM", () => {
  assert.match(readSource("./apm-spec-validate.ts"), /sendHiddenPrompt\(pi, "apm-spec-validate", prompt\)/);
  assert.match(readSource("./apm-spec-score.ts"), /sendHiddenPrompt\(pi, "apm-spec-score", prompt\)/);
});

test("spec-validate is a gate and spec-score is a measurement; both reuse the canonical quality procedure", () => {
  const validate = readSource("./prompts/apm-spec-validate.md");
  const score = readSource("./prompts/apm-spec-score.md");
  const canonical = readSource("./prompts/apm-spec-quality.md");
  // Both inject the canonical 11-dimension analysis by reference — no
  // duplicated evaluation logic (same pattern as the drift procedure).
  for (const prompt of [validate, score]) {
    assert.match(prompt, /core\/prompts\/apm-spec-quality\.md/);
    assert.doesNotMatch(prompt, /11 Dimensions/);
  }
  // Gate semantics: errors block, warnings pass unless strict, never edit.
  assert.match(validate, /--strict/);
  assert.match(validate, /REJECTED/);
  assert.match(validate, /Never.*advance a spec by editing it here/);
  // Score semantics: weighted 0-100, threshold, not a gate.
  assert.match(score, /--threshold/);
  assert.match(score, /IEEE 830 \/ ISO 29148/);
  assert.match(score, /not a gate/);
  // Canonical core owns the dimensions and their weights/severities.
  assert.match(canonical, /Implementation Leakage/);
  assert.match(canonical, /Constitution Adherence/);
  assert.match(canonical, /Scope Alignment/);
  assert.match(canonical, /Never.*edit the spec to improve a score/);
});

test("router dispatches every subcommand to a dedicated handler", () => {
  const source = readSource("./apm-command.ts");
  assert.match(source, /handleInit\(ctx\)/);
  assert.match(source, /handlePrd\(pi, args, ctx\)/);
  assert.match(source, /handleSpec\(pi, args, ctx\)/);
  assert.match(source, /handleClarify\(pi, args, ctx\)/);
  assert.match(source, /handleTechPlan\(pi, args, ctx\)/);
  assert.match(source, /handleBreakdown\(pi, args, ctx\)/);
  assert.match(source, /handleDistill\(pi, args, ctx\)/);
  assert.match(source, /handleImplement\(pi, args, ctx\)/);
  assert.match(source, /handleDrift\(pi, args, ctx\)/);
  assert.match(source, /handleSpecValidate\(pi, args, ctx\)/);
  assert.match(source, /handleSpecScore\(pi, args, ctx\)/);
  assert.match(source, /apm-init|apm-prd|apm-spec|apm-clarify|apm-tech-plan|apm-breakdown|apm-implement|apm-distill/);
});

test("handleClarify rejects empty and workspace-less input", () => {
  const source = readSource("./apm-clarify.ts");
  assert.match(source, /if \(!hasApmWorkspace\(ctx\)\)/);
  assert.match(source, /if \(!rest\)/);
  assert.match(source, /Usage: \/apm clarify/);
});

test("handleTechPlan rejects empty and workspace-less input", () => {
  const source = readSource("./apm-tech-plan.ts");
  assert.match(source, /if \(!hasApmWorkspace\(ctx\)\)/);
  assert.match(source, /if \(!rest\)/);
  assert.match(source, /Usage: \/apm tech-plan/);
});

test("handleBreakdown rejects empty and workspace-less input", () => {
  const source = readSource("./apm-breakdown.ts");
  assert.match(source, /if \(!hasApmWorkspace\(ctx\)\)/);
  assert.match(source, /if \(!rest\)/);
  assert.match(source, /Usage: \/apm breakdown/);
});

test("handleImplement rejects empty and workspace-less input", () => {
  const source = readSource("./apm-implement.ts");
  assert.match(source, /if \(!hasApmWorkspace\(ctx\)\)/);
  assert.match(source, /if \(!rest\)/);
  assert.match(source, /Usage: \/apm implement/);
});

test("handleDistill gates on workspace and defaults empty input to the whole project", () => {
  const source = readSource("./apm-distill.ts");
  assert.match(source, /if \(!hasApmWorkspace\(ctx\)\)/);
  assert.match(source, /const moduleArg = rest \|\| "\.";/);
});

test("prompt templates carry the input prefix and handlers inject bare values", () => {
  // Regression: apm-distill rendered "- module: - module: ." because the
  // handler re-injected the prefix the template already had.
  for (const [name, prefix] of [
    ["apm-breakdown.md", "- plan: {INPUT}"],
    ["apm-implement.md", "- tasks: {INPUT}"],
    ["apm-tech-plan.md", "- feature: {INPUT}"],
    ["apm-distill.md", "- module: {INPUT}"],
  ] as const) {
    const prompt = readSource(`./prompts/${name}`);
    assert.match(prompt, new RegExp(`^${prefix.replace(/[-:\\[\]{}()/*+?.^$|]/g, "\\$&")}$`, "m"));
  }
  const breakdown = readSource("./apm-breakdown.ts");
  assert.doesNotMatch(breakdown, /replace\("\{INPUT\}", `- plan:/);
  const techPlan = readSource("./apm-tech-plan.ts");
  assert.doesNotMatch(techPlan, /replace\("\{INPUT\}", `- feature:/);
  const distill = readSource("./apm-distill.ts");
  assert.doesNotMatch(distill, /replace\("\{INPUT\}", `- module:/);
});

test("implement prompt is an import-and-handoff orchestrator, not an inline executor", () => {
  const prompt = readSource("./prompts/apm-implement.md");
  // Handoff flow: gate, import (dry-run + real), resume on re-import,
  // owner-relayed authorization, exit after handoff.
  assert.match(prompt, /2\. IMPORT/);
  assert.match(prompt, /--dry-run/);
  assert.match(prompt, /RESUME mode/);
  assert.match(prompt, /3\. AUTHORIZATION/);
  assert.match(prompt, /authorize_work_item_implementation/);
  assert.match(prompt, /4\. HANDOFF/);
  // HANDOFF exits; no attached polling loop.
  assert.match(prompt, /do not attach a polling\s+loop/);
  // Inline execution machinery is gone: the scheduler owns it.
  assert.doesNotMatch(prompt, /BOUNDARY/);
  assert.doesNotMatch(prompt, /Stub Detection/);
  assert.doesNotMatch(prompt, /^RED:/m);
  // The Input contract is pinned by another test; the orchestrator keeps it.
  assert.match(prompt, /- tasks: \{INPUT\}/);
});

test("parseSpecArgs reads dc:especificar-style fields", () => {
  const fields = parseSpecArgs("Type: COMMAND Feature: usuario/CrearUsuario Domain: usuario Requirement: allow users to register with email and password Context: @docs/auth.md");
  assert.equal(fields.type, "COMMAND");
  assert.equal(fields.feature, "usuario/CrearUsuario");
  assert.equal(fields.domain, "usuario");
  assert.equal(fields.requirement, "allow users to register with email and password");
  assert.equal(fields.context, "@docs/auth.md");
});

test("parseSpecArgs leaves requirement unset for field-only input", () => {
  const fields = parseSpecArgs("Type: QUERY Domain: pedido");
  assert.equal(fields.type, "QUERY");
  assert.equal(fields.requirement, undefined);
});

test("parseSpecArgs keeps free text until the next known key", () => {
  const fields = parseSpecArgs("Requirement: register with email feature/registro and password Type: COMMAND");
  assert.equal(fields.requirement, "register with email feature/registro and password");
  assert.equal(fields.type, "COMMAND");
});

test("parseSpecArgs merges repeated Context values", () => {
  const fields = parseSpecArgs("Requirement: create user Context: @docs/auth.md Context: @docs/roles.md");
  assert.equal(fields.context, "@docs/auth.md @docs/roles.md");
});

test("parseSpecArgs is case-insensitive on keys only", () => {
  const fields = parseSpecArgs("type: EVENT requirement: emit user created");
  assert.equal(fields.type, "EVENT");
  assert.equal(fields.requirement, "emit user created");
});

test("parseSpecArgs returns empty for prose without fields", () => {
  assert.deepEqual(parseSpecArgs("just some plain text"), {});
});
