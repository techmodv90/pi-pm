import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

// Retained-worktree stale-base constraint (RLB-GAP-003): a pack-keyed worktree
// reused for a new run must sit at the run's declared base_commit — claims stamp
// base_commit at claim time, and the candidate patch is later applied against
// that exact commit. No base given = legacy call sites keep the old no-reset
// reuse semantics; dirty worktrees refuse loudly rather than silently mis-basing.
export async function alignRetainedWorktreeBase(worktree: string, baseCommit?: string): Promise<void> {
  if (!baseCommit) return;
  const head = (await execFileAsync("git", ["-C", worktree, "rev-parse", "HEAD"], { encoding: "utf8" })).stdout.trim();
  if (head === baseCommit) return;
  const dirty = (await execFileAsync("git", ["-C", worktree, "status", "--porcelain"], { encoding: "utf8" })).stdout.trim().length > 0;
  if (dirty) throw new Error(`retained worktree ${worktree} is dirty at stale base ${head.slice(0, 12)}; run base is ${baseCommit.slice(0, 12)} — reconcile the uncommitted work manually before relaunching (RLB-GAP-003)`);
  try {
    await execFileAsync("git", ["-C", worktree, "merge", "--ff-only", baseCommit], { encoding: "utf8" });
  } catch {
    throw new Error(`retained worktree ${worktree} at ${head.slice(0, 12)} cannot fast-forward to run base ${baseCommit.slice(0, 12)} (non-ancestor); RLB-GAP-003 refuses to rewrite it`);
  }
}
