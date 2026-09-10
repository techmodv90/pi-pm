/**
 * /apm implement — phase-by-phase TDD execution of an approved .tasks.md,
 * adopted from don-cheli-sdd dc:implementar (itself adapted from speckit
 * implement + gsd-build checkpoint taxonomy). Docker requirement removed:
 * tests, lint, and build run with the project's native commands detected
 * from manifests. Checkpoints and the 3-failure stop-loss are kept.
 */

import type { ExtensionAPI, ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import {
  assertApprovedState,
  hasApmWorkspace,
  loadPrompt,
  resolveArtifactPath,
  runGate,
  sendHiddenPrompt,
} from "./apm-shared.ts";

export function handleImplement(pi: ExtensionAPI, args: string, ctx: ExtensionCommandContext): void {
  if (!hasApmWorkspace(ctx)) {
    ctx.ui.notify("No APM workspace found. Run /apm init first.", "warning");
    return;
  }
  const rest = args.trim().replace(/^implement\b\s*/, "");
  if (!rest) {
    ctx.ui.notify(
      "Usage: /apm implement <path-to-.tasks.md | Tasks: <domain/Name>> [--phase <n>] [--dry]",
      "error",
    );
    return;
  }
  // Gate: the task list must carry an APPROVED stamp (hash-bound).
  const tasksPath = resolveArtifactPath(ctx.cwd, rest, ".tasks.md");
  if (!runGate(ctx, tasksPath, assertApprovedState)) return;
  // Loaded at call time so prompt edits apply without an extension reload.
  const prompt = loadPrompt("apm-implement.md").replace("{INPUT}", rest);
  sendHiddenPrompt(pi, "apm-implement", prompt);
}
