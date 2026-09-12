import { execFileSync } from "node:child_process";
import { realpathSync } from "node:fs";
import type { SubagentResult, SubagentWorkspace } from "./types.ts";

export function gitText(cwd: string, args: string[]): string {
  return execFileSync("git", args, { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] }).trim();
}

export function gitStatusOrUndefined(cwd: string): string | undefined {
  try { return gitText(cwd, ["status", "--porcelain=v1", "--untracked-files=all"]); } catch { return undefined; }
}

export function workspaceDiagnostics(cwd: string): SubagentWorkspace {
  const canonical = realpathSync(cwd);
  const gitToplevel = realpathSync(gitText(canonical, ["rev-parse", "--show-toplevel"]));
  if (gitToplevel !== canonical) throw new Error(`worker worktree invariant failed: child_process_cwd=${canonical} git_toplevel=${gitToplevel}`);
  return {
    assignedWorktree: canonical,
    childProcessCwd: canonical,
    bashCwd: canonical,
    readToolRoot: canonical,
    editToolRoot: canonical,
    writeToolRoot: canonical,
    applyPatchRoot: canonical,
    gitToplevel,
    head: gitText(canonical, ["rev-parse", "HEAD"]),
    statusBefore: gitText(canonical, ["status", "--short"]),
    statusAfter: "",
    diffStatAfter: "",
  };
}

export function updateUsage(result: SubagentResult, message: any): void {
  if (message.role !== "assistant") return;
  result.usage.turns++;
  const usage = message.usage;
  if (!usage) return;
  result.usage.input += usage.input || 0;
  result.usage.output += usage.output || 0;
  result.usage.cacheRead += usage.cacheRead || 0;
  result.usage.cacheWrite += usage.cacheWrite || 0;
  result.usage.cost += usage.cost?.total || 0;
  result.usage.contextTokens = usage.totalTokens || 0;
  if (!result.model && message.model) result.model = message.model;
  result.stopReason = message.stopReason || result.stopReason;
  result.errorMessage = message.errorMessage || result.errorMessage;
}
