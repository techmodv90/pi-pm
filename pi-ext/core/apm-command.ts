/**
 * /apm command router for the APM (Agent Project Management) extension.
 * Subcommand handlers live in apm-init.ts, apm-prd.ts, apm-spec.ts,
 * apm-clarify.ts, and apm-tech-plan.ts.
 * init: create the per-project APM workspace (.apm/prd, .apm/specs git-tracked, .apm/state ignored).
 * prd: generate a PRD from multi-source input at .apm/prd/prd-v<major.minor>.md.
 * spec: generate a Gherkin spec (.feature) + DBML from a typed requirement at .apm/specs/.
 * clarify: review a spec for ambiguity and upgrade @draft → @ready on a passing Auto-QA.
 * tech-plan: generate a technical blueprint (.plan.md) from a @ready spec.
 * breakdown: generate an executable task list (.tasks.md) from an approved
 *            blueprint.
 * implement: execute an approved .tasks.md phase by phase with TDD, native
 *            commands, and human checkpoints (no Docker).
 * distill: reverse-engineer compact specs/contracts (.distilled.md) from
 *          existing code.
 */

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { handleBreakdown } from "./apm-breakdown.ts";
import { handleClarify } from "./apm-clarify.ts";
import { handleDistill } from "./apm-distill.ts";
import { handleInit } from "./apm-init.ts";
import { handleImplement } from "./apm-implement.ts";
import { handlePrd } from "./apm-prd.ts";
import { handleSpec } from "./apm-spec.ts";
import { handleTechPlan } from "./apm-tech-plan.ts";

const USAGE = "APM (Agent Project Management):\n  /apm init            Create .apm/prd, .apm/specs (tracked) and ignored .apm/state\n  /apm prd             Generate a PRD (interactive) at .apm/prd/prd-v<major.minor>.md\n  /apm prd <sources>   Generate a PRD from a description, brief, or Figma link\n                       flags: --figma <link>  --brief <file>  --template <name>  --version <n>\n                       (--version pins the project version: writes prd-v<n>.md)\n  /apm spec <fields>   Generate a Gherkin spec (.feature) at .apm/specs/<domain>/<Name>.feature\n                       fields: Type: <COMMAND|QUERY|EVENT>  Feature: <domain/Name>\n                               Domain: <domain>  Requirement: <text>  Context: @<file>\n  /apm clarify <spec>  Review a spec for ambiguity, run Auto-QA, and upgrade\n                       @draft → @ready on a passing verdict\n                       spec: a .feature path or Feature: <domain/Name>\n  /apm tech-plan <spec> Generate a technical blueprint (.plan.md) beside a\n                       @ready spec, ratify its DBML, and gate on the APM constitution\n                       spec: a .feature path or Feature: <domain/Name>\n  /apm breakdown <plan> Generate an executable task list (.tasks.md) beside an\n                       approved blueprint with TDD and parallelism markers\n                       plan: a .plan.md path or Blueprint: <domain/Name>\n  /apm implement <tasks> Execute an approved .tasks.md phase by phase with\n                       TDD (RED→GREEN→REFACTOR), native test/lint/build\n                       commands, and human checkpoints — no Docker\n                       tasks: a .tasks.md path or Tasks: <domain/Name>\n                       flags: --phase <n>  --dry\n  /apm distill [module] Reverse-engineer compact specs, contracts, and\n                       business rules from existing code into a\n                       .distilled.md under .apm/specs/ (default: whole project)\n                       flags: --format gherkin | contracts | summary";

export function registerApmCommand(pi: ExtensionAPI) {
  pi.registerCommand("apm", {
    description: "APM: init project workspace, generate a PRD, generate a Gherkin spec, clarify a spec, generate a technical blueprint, break an approved blueprint into tasks, implement an approved task list with TDD, or distill specs from existing code",
    handler: async (args, ctx) => {
      const sub = args.trim().split(/\s+/)[0] || "help";

      if (sub === "help") {
        ctx.ui.notify(USAGE, "info");
        return;
      }
      if (sub === "init") {
        handleInit(ctx);
        return;
      }
      if (sub === "prd") {
        handlePrd(pi, args, ctx);
        return;
      }
      if (sub === "spec") {
        handleSpec(pi, args, ctx);
        return;
      }
      if (sub === "clarify") {
        handleClarify(pi, args, ctx);
        return;
      }
      if (sub === "tech-plan") {
        handleTechPlan(pi, args, ctx);
        return;
      }
      if (sub === "breakdown") {
        handleBreakdown(pi, args, ctx);
        return;
      }
      if (sub === "implement") {
        handleImplement(pi, args, ctx);
        return;
      }
      if (sub === "distill") {
        handleDistill(pi, args, ctx);
        return;
      }

      ctx.ui.notify(`Unknown subcommand: ${sub}. Try /apm help`, "error");
    },
  });
}
