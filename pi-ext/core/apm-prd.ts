/** /apm prd — generate a PRD from multi-source input at .apm/prd/prd-v<major.minor>.md. */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { hasApmWorkspace, loadPrompt, sendHiddenPrompt } from "./apm-shared.ts";

/** Split /apm prd args into a sources/options block for the prompt. */
function parsePrdArgs(rest: string[]): string {
  const lines: string[] = [];
  for (let i = 0; i < rest.length; i++) {
    const token = rest[i];
    if (token === "--figma" && rest[i + 1]) { lines.push(`- Figma link: ${rest[++i]}`); continue; }
    if (token === "--brief" && rest[i + 1]) { lines.push(`- Product brief: ${rest[++i]}`); continue; }
    if (token === "--template" && rest[i + 1]) { lines.push(`- Template: ${rest[++i]}`); continue; }
    if (token === "--version" && rest[i + 1]) { lines.push(`- Pin project version (writes prd-v${rest[++i]}.md): ${rest[i]}`); continue; }
    lines.push(`- Description: ${rest.slice(i).join(" ")}`);
    break;
  }
  return lines.join("\n");
}

export function handlePrd(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^prd\b\s*/, "").split(/\s+/).filter(Boolean);
  const sources = rest.length > 0
    ? `Provided sources:\n${parsePrdArgs(rest)}`
    : "None provided. Evidence-first mode: before interviewing anyone, scan the current working directory — the existing codebase (structure, stack, entry points, tests) and any documents (README, docs/, .apm/, plans, briefs). Draft the PRD from that evidence. Raise questions to the owner only for gaps or conflicts the evidence cannot resolve. Only if neither a codebase nor documents exist, interview the owner first (problem, audience, constraints, success, assets, anti-scope).";
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-prd.md").replace("{SOURCES}", sources);
  sendHiddenPrompt(pi, "apm-prd", prompt);
}
