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

test("hides the full distill prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-distill.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-distill", prompt\)/);
});

test("hides the full implement prompt while retaining it for the LLM", () => {
  const source = readSource("./apm-implement.ts");
  assert.match(source, /sendHiddenPrompt\(pi, "apm-implement", prompt\)/);
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
