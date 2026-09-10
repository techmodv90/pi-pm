/**
 * /apm command router for the APM (Agent Project Management) extension.
 * Subcommand handlers live in apm-init.ts, apm-prd.ts, apm-spec.ts,
 * apm-clarify.ts, and apm-tech-plan.ts.
 * init: create the per-project APM workspace (.apm/prd, .apm/specs git-tracked, .apm/state ignored).
 * prd: generate a PRD from multi-source input at .apm/prd/prd-v<major.minor>.md.
 * spec: generate a Gherkin spec (.feature) + DBML from a typed requirement at .apm/specs/features/.
 * propose: write a change proposal (RFC) at .apm/proposals/ before a spec is
 *          written — Intent/Scope/Approach/Risks, gated on owner approval.
 * explore: investigate a codebase area with evidence-cited findings at
 *          .apm/explore/ before any proposal (Assumptions mode default);
 * clarify: review a spec for ambiguity and upgrade @draft → @ready on a passing Auto-QA.
 * tech-plan: generate a technical blueprint (.plan.md) from a @ready spec.
 * design: write a technical design document (.apm/design/) with ADRs,
 *         trade-offs, diagrams, and a complexity table — the WHY next to
 *         tech-plan's WHAT.
 * breakdown: generate an executable task list (.tasks.md) from an approved
 *            blueprint.
 * implement: execute an approved .tasks.md phase by phase with TDD, native
 *            commands, and human checkpoints (no Docker).
 * distill: reverse-engineer compact specs/contracts (.distilled.md) from
 *          existing code.
 * drift: run the canonical spec-drift conformance check (.apm/specs vs
 *          real test coverage) — CRÍTICO/WARNING gaps become Bug Work Items.
 *          /apm implement and /apm review (epic tier) inject it by reference.
 */

import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { handleBreakdown } from "./apm-breakdown.ts";
import { handleClarify } from "./apm-clarify.ts";
import { handleApprove } from "./apm-approve.ts";
import { handleDesign } from "./apm-design.ts";
import { handleDistill } from "./apm-distill.ts";
import { handleDrift } from "./apm-drift.ts";
import { handleExplore } from "./apm-explore.ts";
import { handleInit } from "./apm-init.ts";
import { handleImplement } from "./apm-implement.ts";
import { handlePrd } from "./apm-prd.ts";
import { handlePropose } from "./apm-propose.ts";
import { handlePseudocode } from "./apm-pseudocode.ts";
import { handleSpec } from "./apm-spec.ts";
import { handleSpecScore } from "./apm-spec-score.ts";
import { handleSpecValidate } from "./apm-spec-validate.ts";
import { handleTechPlan } from "./apm-tech-plan.ts";

const USAGE = "APM (Agent Project Management):\n  /apm init            Create .apm/prd, .apm/specs (tracked) and ignored .apm/state\n  /apm prd             Generate a PRD (interactive) at .apm/prd/prd-v<major.minor>.md\n  /apm prd <sources>   Generate a PRD from a description, brief, or Figma link\n                       flags: --figma <link>  --brief <file>  --template <name>  --version <n>\n                       (--version pins the project version: writes prd-v<n>.md)\n  /apm spec <fields>   Generate a Gherkin spec (.feature) at .apm/specs/features/<domain>/<Name>.feature\n                       fields: Type: <COMMAND|QUERY|EVENT>  Feature: <domain/Name>\n                               Domain: <domain>  Requirement: <text>  Context: @<file>\n  /apm explore <area>  Investigate a codebase area with evidence-cited\n                       findings, updating its section of the single\n                       .apm/architecture.md in place (coverage table with\n                       per-section verified_at) before any proposal —\n                       Assumptions mode by default; feeds /apm propose.\n                       Empty area = overall architecture.\n  /apm propose <change> Write a change proposal (RFC) at .apm/proposals/<slug>-proposal.md\n                       before any spec — Intent (WHY), Scope (WHAT, included AND\n                       excluded), Approach (HOW), Risks, preliminary estimate.\n                       Gated: /apm approve flips the proposal to APPROVED\n                       (hash-bound state) before /apm spec accepts it.\n  /apm approve <artifact>\n                       Present an artifact honestly and flip its approval\n                       gate on owner confirmation: writes\n                       State: APPROVED hash=<sha1-of-body> so later edits\n                       invalidate the gate. Applies to proposals, designs,\n                       and .tasks.md (specs are gated by /apm clarify @ready).\n  /apm clarify <spec>  Review a spec for ambiguity, run Auto-QA, and upgrade\n                       @draft → @ready on a passing verdict\n                       spec: a .feature path or Feature: <domain/Name>\n  /apm tech-plan <spec> Generate a technical blueprint (.plan.md) beside a\n                       @ready spec, ratify its DBML, and gate on the APM constitution\n                       spec: a .feature path or Feature: <domain/Name>\n  /apm design <plan>   Write a technical design doc at .apm/design/<feature>-design.md\n                       with ADRs (context, evaluated options, consequences),\n                       flow diagrams, and a complexity table — the WHY next to\n                       tech-plan's WHAT. Owner-reviewed before /apm breakdown.\n  /apm pseudocode <scope>\n                       Sketch technology-agnostic logic from the domain's\n                       Gherkin specs (flows in WHEN/IF/FOR-EACH steps,\n                       invariants, edge cases) into\n                       .apm/pseudocode/<slug>-pseudocode.md — the sketch\n                       before the blueprints. Feeds /apm design.\n                       scope: a domain directory, a .feature path, or a slug\n  /apm breakdown <plan> Generate an executable task list (.tasks.md) beside an\n                       approved blueprint with TDD and parallelism markers\n                       plan: a .plan.md path or Blueprint: <domain/Name>\n  /apm implement <tasks> Execute an approved .tasks.md phase by phase with\n                       TDD (RED→GREEN→REFACTOR), native test/lint/build\n                       commands, and human checkpoints — no Docker\n                       tasks: a .tasks.md path or Tasks: <domain/Name>\n                       flags: --phase <n>  --dry\n                       Gate: the .tasks.md must be /apm approve'd first.\n  /apm distill [module] Reverse-engineer compact specs, contracts, and\n                       business rules from existing code into a\n                       .distilled.md under .apm/specs/ (default: whole project)\n                       flags: --format gherkin | contracts | summary\n  /apm drift [scope]   Run the spec-drift conformance check: Gherkin specs\n                       (.apm/specs/**/*.feature) vs real test coverage.\n                       CRÍTICO/WARNING gaps become Bug Work Items.\n                       scope: a .feature path, a domain directory, or empty\n                       flags: --severity critical  --format json\n  /apm archive <feature>\n                       Close a completed feature: verify the @implemented\n                       tag (fail-closed), move its artifacts to\n                       .apm/specs/archive/<domain>/<Feature>/, write\n                       metadata.json (dates, scenario/test/file counts),\n                       record the decision in .apm/decisions.md, and clean\n                       up temporary files.\n                       feature: a .feature path, Feature: <domain/Name>,\n                       or --all for every @implemented feature\n/apm spec-validate [scope]\n                       Validate spec quality before planning: 11 checks\n(completeness, leakage, traceability, constitution,\nscope, ...) with PASS/WARN/ERROR verdicts. Errors\nblock tech-plan/breakdown.\nscope: a .feature path, a domain directory, or empty\nflags: --strict  --format json\n  /apm spec-score [scope]\n                       Score spec quality 0-100 across the same 11\nweighted dimensions, mapped to IEEE 830 / ISO 29148,\nwith actionable fixes and estimated point gains.\nMeasurement, not a gate.\nscope: a .feature path, a domain directory, or empty\nflags: --threshold <n> (default 80)";

export function registerApmCommand(pi: ExtensionAPI) {
  pi.registerCommand("apm", {
    description: "APM: init project workspace, generate a PRD, explore a codebase area, propose a change, generate a Gherkin spec, clarify a spec, generate a technical blueprint, design it with ADRs, sketch technology-agnostic pseudocode from a domain's specs, approve gated artifacts, break an approved blueprint into tasks, implement an approved task list with TDD, distill specs from existing code, run the spec-drift conformance check, run the spec-drift conformance check, validate spec quality, score spec quality, or archive completed specs",
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
      if (sub === "explore") {
        handleExplore(pi, args, ctx);
        return;
      }
      if (sub === "propose") {
        handlePropose(pi, args, ctx);
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
      if (sub === "design") {
        handleDesign(pi, args, ctx);
        return;
      }
      if (sub === "approve") {
        handleApprove(pi, args, ctx);
        return;
      }
      if (sub === "pseudocode") {
        handlePseudocode(pi, args, ctx);
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
      if (sub === "drift") {
        handleDrift(pi, args, ctx);
        return;
      }
      if (sub === "spec-validate") {
        handleSpecValidate(pi, args, ctx);
        return;
      }
      if (sub === "spec-score") {
        handleSpecScore(pi, args, ctx);
        return;
      }

      ctx.ui.notify(`Unknown subcommand: ${sub}. Try /apm help`, "error");
    },
  });
}
