/**
 * /apm spec — generate a Gherkin spec (.feature) + DBML from a typed
 * dc:especificar-style requirement, written under .apm/specs/features/<domain>/.
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { existsSync, mkdirSync } from "node:fs";
import { resolve } from "node:path";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

type SpecField = "type" | "feature" | "domain" | "requirement" | "context";

const SPEC_FIELD_KEYS: Record<string, SpecField> = {
  type: "type",
  feature: "feature",
  domain: "domain",
  requirement: "requirement",
  context: "context",
};

/**
 * Parse /apm spec Key: value fields (dc:especificar format). Each value runs
 * until the next known key; repeated keys merge with a space. Key names are
 * matched case-insensitively, but only the exact known key words match, so
 * arbitrary prose never does (a literal "Context:" inside prose would split —
 * acceptable for reserved field labels).
 */
export function parseSpecArgs(rest: string): Partial<Record<SpecField, string>> {
  const keyPattern = Object.keys(SPEC_FIELD_KEYS).join("|");
  const re = new RegExp(`\\b(${keyPattern})\\s*:`, "gi");
  const hits: { field: SpecField; matchStart: number; valueStart: number }[] = [];
  let m: RegExpExecArray | null;
  while ((m = re.exec(rest)) !== null) {
    hits.push({ field: SPEC_FIELD_KEYS[m[1].toLowerCase()], matchStart: m.index, valueStart: re.lastIndex });
  }
  const out: Partial<Record<SpecField, string>> = {};
  for (let i = 0; i < hits.length; i++) {
    const value = rest.slice(hits[i].valueStart, i + 1 < hits.length ? hits[i + 1].matchStart : rest.length).trim();
    const f = hits[i].field;
    if (f === "context") out.context = out.context ? `${out.context} ${value}` : value;
    else if (out[f] === undefined) out[f] = value;
    else out[f] = `${out[f]} ${value}`;
  }
  return out;
}

export function handleSpec(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^spec\b\s*/, "");
  const fields = parseSpecArgs(rest);
  if (!fields.requirement) {
    if (rest) {
      // No structured keys — treat the whole input as the requirement text
      // and let the prompt infer the remaining fields (dc:especificar behavior).
      fields.requirement = rest;
    } else if (!existsSync(resolve(ctx.cwd, ".apm", "prd"))) {
      ctx.ui.notify(
        "Missing Requirement field (and no PRD found in .apm/prd/). Usage: /apm spec [Type: <COMMAND|QUERY|EVENT>] [Feature: <domain/Name>] [Domain: <domain>] [Context: @<file>] Requirement: <text>",
        "error",
      );
      return;
    }
    // Empty input with a PRD present — the prompt analyzes the PRD directly.
  }
  mkdirSync(resolve(ctx.cwd, ".apm", "specs"), { recursive: true });
  const input = Object.entries(fields).map(([k, v]) => `- ${k}: ${v}`).join("\n") || "(none — analyze the PRD directly)";
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-spec.md").replace("{INPUT}", input);
  sendHiddenPrompt(pi, "apm-spec", prompt);
}
