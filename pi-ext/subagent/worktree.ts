import { execFile, execFileSync } from "node:child_process";
import { existsSync, mkdirSync, realpathSync, statSync } from "node:fs";
import { isMutationStage } from "../pipeline/report-parsing.ts";
import { alignRetainedWorktreeBase } from "./worktree-align.ts";
import { randomUUID } from "node:crypto";
import { basename, join } from "node:path";
import { promisify } from "node:util";
import type { SubagentResult, SubagentSpec } from "./types.ts";
import { finalAssistantText } from "./spawn-utils.ts";

const execFileAsync = promisify(execFile);

/** Retention horizon aligning the orphan sweep with GAP-137's 24h session TTL. */
export const ORPHANED_WORKTREE_MAX_AGE_MS = 24 * 60 * 60 * 1000;

export async function prepareSubagentWorktree(cwd: string, initialPatchPath?: string, runId: string = randomUUID(), worktreeKey: string = runId, baseCommit?: string): Promise<{ runId: string; cwd: string; reused: boolean }> {
  const worktreeRoot = join(cwd, ".pi", "worktrees");
  mkdirSync(worktreeRoot, { recursive: true, mode: 0o700 });
  const worktree = join(worktreeRoot, worktreeKey);
  // Durable worker worktree constraint (RLB-GAP-001): a pack-keyed worktree left
  // by a retained transient failure is reused in place — no reset, no patch
  // reapply — so the retry resumes the prior attempt's partial work.
  if (existsSync(worktree) && existsSync(join(worktree, ".git"))) {
    const branch = `pi-agent-${worktreeKey}`;
    const registration = execFileSync("git", ["worktree", "list", "--porcelain"], { cwd, encoding: "utf8" })
      .trim().split(/\n\n+/).find((block) => {
        const registeredPath = block.match(/^worktree (.+)$/m)?.[1];
        return registeredPath && (existsSync(registeredPath) ? realpathSync(registeredPath) : registeredPath) === realpathSync(worktree);
      });
    const registeredBranch = registration?.match(/^branch refs\/heads\/(.+)$/m)?.[1];
    if (registration && registeredBranch === branch) {
      await alignRetainedWorktreeBase(worktree, baseCommit);
      return { runId, cwd: worktree, reused: true };
    }
    throw new Error(`refusing to reuse unregistered or foreign-branch worktree: ${worktree}`);
  }
  if (existsSync(worktree)) throw new Error(`refusing to overwrite non-worktree path: ${worktree}`);
  await execFileAsync("git", ["worktree", "add", "-b", `pi-agent-${worktreeKey}`, worktree, baseCommit || "HEAD"], { cwd });
  try {
    if (initialPatchPath && statSync(initialPatchPath).size > 0) {
      try {
        await execFileAsync("git", ["apply", "--check", initialPatchPath], { cwd: worktree });
        await execFileAsync("git", ["apply", initialPatchPath], { cwd: worktree });
      } catch (error) {
        // Review recovery: an integrated candidate patch is already in the base; review that base directly.
        try {
          await execFileAsync("git", ["apply", "--reverse", "--check", initialPatchPath], { cwd: worktree });
        } catch {
          throw error;
        }
      }
    }
  } catch (error) {
    removeSubagentWorktree(cwd, worktree, worktreeKey);
    throw error;
  }
  return { runId, cwd: worktree, reused: false };
}

export function removeSubagentWorktree(cwd: string, worktree: string, runId: string): void {
  const branch = `pi-agent-${runId}`;
  if (basename(worktree) !== runId) throw new Error(`refusing to remove unowned worktree: ${worktree}`);
  const canonicalWorktree = existsSync(worktree) ? realpathSync(worktree) : worktree;
  const registration = execFileSync("git", ["worktree", "list", "--porcelain"], { cwd, encoding: "utf8" })
    .trim().split(/\n\n+/).find((block) => {
      const registeredPath = block.match(/^worktree (.+)$/m)?.[1];
      return registeredPath && (existsSync(registeredPath) ? realpathSync(registeredPath) : registeredPath) === canonicalWorktree;
    });
  if (registration) {
    const registeredBranch = registration.match(/^branch refs\/heads\/(.+)$/m)?.[1];
    if (registeredBranch !== branch) throw new Error(`refusing to remove unowned worktree branch: ${registeredBranch || "detached"}`);
    execFileSync("git", ["worktree", "remove", "--force", worktree], { cwd, stdio: "pipe" });
  } else if (existsSync(worktree)) {
    throw new Error(`refusing to remove unregistered worktree: ${worktree}`);
  }
  try { execFileSync("git", ["show-ref", "--verify", `refs/heads/${branch}`], { cwd, stdio: "pipe" }); }
  catch { return; }
  execFileSync("git", ["branch", "-D", branch], { cwd, stdio: "pipe" });
}

export function cleanupOrphanedSubagentWorktrees(cwd: string, activeRunIds: ReadonlySet<string>, orphanAgeMs: number = ORPHANED_WORKTREE_MAX_AGE_MS): void {
  const cutoff = Date.now() - orphanAgeMs;
  const blocks = execFileSync("git", ["worktree", "list", "--porcelain"], { cwd, encoding: "utf8" }).trim().split(/\n\n+/);
  for (const block of blocks) {
    const path = block.match(/^worktree (.+)$/m)?.[1];
    const branch = block.match(/^branch refs\/heads\/pi-agent-(.+)$/m)?.[1];
    if (!path || !branch || activeRunIds.has(branch) || basename(path) !== branch) continue;
    // Durable worker worktree constraint (RLB-GAP-001): pack-keyed worktrees are
    // retained across transient worker failures, so a worktree that is not part
    // of an active claim is only reclaimed once it is older than the retention
    // horizon (aligned with the GAP-137 24h session TTL). Age is taken from the
    // worktree directory mtime, refreshed by every retained attempt.
    let ageOk = true;
    try { ageOk = statSync(path).mtimeMs < cutoff; } catch { ageOk = false; }
    if (!ageOk) continue;
    removeSubagentWorktree(cwd, path, branch);
  }
}

// Durable worker worktree constraint (RLB-GAP-001): a mutation-stage child that
// died before emitting its completion report (provider stream loss, watchdog
// kill, deadline kill, mid-run exit) retains its pack-keyed worktree so the
// retry resumes the prior attempt's partial work; a run that emitted a report is
// deterministic and cleans up exactly as GAP-091/096 required.
export function retainWorktreeForResume(stage: string | undefined, result: SubagentResult): boolean {
  if (!stage || !isMutationStage(stage)) return false;
  if (result.stopReason === "aborted") return false;
  // Retain every report-less death: nonzero exits, watchdog/deadline kills,
  // and exit-0 stream cuts (the runner defaults stopReason to "end" on exit 0,
  // so stopReason cannot distinguish a clean finish from a provider stream
  // that ended without the worker emitting its completion report — presence
  // of the report in the final assistant text is the only reliable signal).
  return !/<completion_report\b/.test(finalAssistantText(result.messages || []));
}

// Candidate-state restore constraint: a retry must run against the same attested
// candidate as the original launch. git reset --hard HEAD alone discards the
// uncommitted initial patch that prepareSubagentWorktree applied, so after the
// reset we reapply and revalidate spec.initialPatchPath (mirroring the
// reverse-check integration fallback used at preparation time).
export function resetSubagentWorktreeToCandidate(spec: SubagentSpec, assignedWorktree: string, cwd: string): void {
  execFileSync("git", ["-C", assignedWorktree, "reset", "--hard", "HEAD"], { cwd, stdio: "pipe" });
  // Retry-state cleanliness constraint: git reset --hard leaves untracked files
  // from the failed attempt in place, so a retry could inherit generated or newly
  // created artifacts and diverge from the attested candidate. Drop untracked
  // paths before reapplying the patch; no -x, ignored build output stays.
  execFileSync("git", ["-C", assignedWorktree, "clean", "-fd"], { cwd, stdio: "pipe" });
  if (!spec.initialPatchPath || statSync(spec.initialPatchPath).size === 0) return;
  try {
    execFileSync("git", ["apply", "--check", spec.initialPatchPath], { cwd: assignedWorktree, stdio: "pipe" });
    execFileSync("git", ["apply", spec.initialPatchPath], { cwd: assignedWorktree, stdio: "pipe" });
  } catch (error) {
    // Review recovery: mirror preparation; the candidate patch may already be
    // integrated in the base commit, in which case applying is a no-op.
    try { execFileSync("git", ["apply", "--reverse", "--check", spec.initialPatchPath], { cwd: assignedWorktree, stdio: "pipe" }); }
    catch { throw error; }
  }
}
