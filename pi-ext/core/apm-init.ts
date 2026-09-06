/** /apm init — create the per-project APM workspace. */

import type { ExtensionCommandContext } from "@mariozechner/pi-coding-agent";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

function ensureGitignoreEntry(projectRoot: string): boolean {
  const gitignorePath = resolve(projectRoot, ".gitignore");
  const entry = ".apm/state/";
  let current = "";
  try {
    current = readFileSync(gitignorePath, "utf8");
  } catch {
    // no .gitignore yet
  }
  if (current.split(/\r?\n/).some((line) => line.trim() === entry)) return false;
  const prefix = current.length > 0 && !current.endsWith("\n") ? "\n" : "";
  const block = `${prefix}# APM runtime state\n${entry}\n`;
  writeFileSync(gitignorePath, current + block);
  return true;
}

export function handleInit(ctx: ExtensionCommandContext): void {
  const prdDir = resolve(ctx.cwd, ".apm", "prd");
  const specsDir = resolve(ctx.cwd, ".apm", "specs");
  const stateDir = resolve(ctx.cwd, ".apm", "state");
  mkdirSync(prdDir, { recursive: true });
  mkdirSync(specsDir, { recursive: true });
  mkdirSync(stateDir, { recursive: true });
  const added = ensureGitignoreEntry(ctx.cwd);
  ctx.ui.notify(`APM workspace ready: .apm/prd/, .apm/specs/, .apm/state/${added ? "; added .apm/state/ to .gitignore" : ""}`, "info");
}
