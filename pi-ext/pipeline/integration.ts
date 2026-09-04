import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execGitIndexWrite, withGitWriteLock } from "../core/cli-helpers.ts";
import { currentFailedReview } from "./report-parsing.ts";
import type { PipelineRun } from "./pipeline-types.ts";

export function runnerRepairEvidence(evidenceJson: string): string {
  const evidence = JSON.parse(evidenceJson);
  if (!evidence || typeof evidence !== "object" || Array.isArray(evidence)) throw new Error("runner repair evidence must be a JSON object");
  if (!evidence.changed_fingerprint) {
    const hash = createHash("sha256");
    for (const file of ["./pipeline-scheduler.ts", "../subagent/runner.ts", "../agents/task-worker.md", "../agents/task-reviewer.md"]) {
      hash.update(readFileSync(new URL(file, import.meta.url)));
    }
    evidence.changed_fingerprint = `runner:${hash.digest("hex")}`;
  }
  return JSON.stringify(evidence);
}

export function verificationEnvironmentFingerprint(cwd: string): string {
  const values = ["NODE_ENV", "CI", "DATABASE_URL", "TEST_DATABASE_URL", "PGHOST", "PGPORT", "PGDATABASE", "PGUSER"]
    .map((name) => `${name}=${process.env[name] || ""}`);
  try { values.push(`HEAD=${execFileSync("git", ["rev-parse", "HEAD"], { cwd, encoding: "utf8" }).trim()}`); } catch { values.push("HEAD=unknown"); }
  try { values.push(`DOCKER=${execFileSync("docker", ["ps", "--format", "{{.Names}}={{.Image}}={{.Status}}"], { cwd, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim()}`); } catch { values.push("DOCKER=unavailable"); }
  return createHash("sha256").update(values.join("\n")).digest("hex");
}

export function repositoryHead(cwd: string): string {
  return execFileSync("git", ["rev-parse", "HEAD"], { cwd, encoding: "utf8" }).trim();
}

export interface AggregateDeliveryState {
  work_item_id: string;
  branch_name: string;
  base_branch: string;
  base_commit: string;
  verified_head: string;
}

export function mergeAggregateBranch(cwd: string, state: AggregateDeliveryState): string {
  return withGitWriteLock(cwd, () => {
    assertCleanGit(cwd);
    const branch = execFileSync("git", ["branch", "--show-current"], { cwd, encoding: "utf8" }).trim();
    if (branch !== state.branch_name || repositoryHead(cwd) !== state.verified_head) throw new Error("delivery branch changed after aggregate verification");
    execFileSync("git", ["fetch", "origin", state.base_branch], { cwd, stdio: "pipe" });
    const remoteBase = execFileSync("git", ["rev-parse", `refs/remotes/origin/${state.base_branch}`], { cwd, encoding: "utf8" }).trim();
    try {
      execFileSync("git", ["merge-base", "--is-ancestor", state.verified_head, remoteBase], { cwd, stdio: "pipe" });
      return remoteBase;
    } catch {}
    if (remoteBase !== state.base_commit) throw new Error(`${state.base_branch} changed after aggregate verification`);

    const tempRoot = mkdtempSync(join(tmpdir(), "pi-aggregate-merge-"));
    const worktree = join(tempRoot, "worktree");
    try {
      execFileSync("git", ["worktree", "add", "--detach", worktree, remoteBase], { cwd, stdio: "pipe" });
      execFileSync("git", ["merge", "--no-ff", state.verified_head, "-m", `merge(${state.work_item_id}): verified aggregate delivery`], { cwd: worktree, stdio: "pipe" });
      const mergeCommit = repositoryHead(worktree);
      execFileSync("git", ["push", "origin", `HEAD:${state.base_branch}`], { cwd: worktree, stdio: "pipe" });
      const remote = execFileSync("git", ["ls-remote", "origin", `refs/heads/${state.base_branch}`], { cwd, encoding: "utf8" }).trim().split(/\s+/)[0];
      if (remote !== mergeCommit) throw new Error(`remote ${state.base_branch} did not confirm merge ${mergeCommit}`);
      return mergeCommit;
    } finally {
      try { execFileSync("git", ["worktree", "remove", "--force", worktree], { cwd, stdio: "pipe" }); } catch {}
      rmSync(tempRoot, { recursive: true, force: true });
    }
  });
}

export function assertReviewBaseCurrent(run: PipelineRun, cwd: string): void {
  if (run.base_commit && run.base_commit !== repositoryHead(cwd)) throw new Error("review base changed before integration; a fresh review is required");
}

export function parseApplyNumstatPaths(output: Buffer | string): Set<string> {
  const fields = output.toString().split("\0");
  const paths = new Set<string>();
  for (let index = 0; index < fields.length && fields[index]; index++) {
    const parts = fields[index]!.split("\t");
    if (parts.length < 3) continue;
    if (parts[2]) paths.add(parts.slice(2).join("\t"));
    else {
      if (fields[index + 1]) paths.add(fields[++index]!);
      if (fields[index + 1]) paths.add(fields[++index]!);
    }
  }
  return paths;
}

export function parsePorcelainPaths(output: Buffer | string): Set<string> {
  const fields = output.toString().split("\0");
  const paths = new Set<string>();
  for (let index = 0; index < fields.length && fields[index]; index++) {
    const entry = fields[index]!;
    const status = entry.slice(0, 2);
    paths.add(entry.slice(3));
    if ((status.includes("R") || status.includes("C")) && fields[index + 1]) index++;
  }
  return paths;
}

export function assertIndexMatchesReviewedPatch(patch: string, cwd: string): void {
  // Reviewed-integration invariant: staged tree must equal HEAD plus the reviewed
  // patch. Byte-comparing the raw patch against `git diff --cached` rejects a
  // mechanically rebased candidate when a sibling integration moved HEAD (blob
  // hashes and hunk offsets shift; bug wi-5dba8c23), so compare trees instead:
  // three-way apply the patch in a temp index seeded from HEAD and require the
  // staged tree to match. Extra unreviewed edits still change the tree and fail.
  const indexDir = mkdtempSync(join(tmpdir(), "task-system-review-index-"));
  const indexPath = join(indexDir, "index");
  try {
    const env = { ...process.env, GIT_INDEX_FILE: indexPath };
    execFileSync("git", ["read-tree", "HEAD"], { cwd, env });
    execFileSync("git", ["apply", "--cached", "--3way", patch], { cwd, env, stdio: "pipe" });
    const reviewedTree = execFileSync("git", ["write-tree"], { cwd, env, encoding: "utf8" }).trim();
    const stagedTree = execFileSync("git", ["write-tree"], { cwd, encoding: "utf8" }).trim();
    if (reviewedTree !== stagedTree) throw new Error("staged integration differs from reviewed candidate patch");
  } catch (error) {
    if (error instanceof Error && error.message === "staged integration differs from reviewed candidate patch") throw error;
    throw new Error("staged integration differs from reviewed candidate patch");
  } finally {
    rmSync(indexDir, { recursive: true, force: true });
  }
}

export function assertCommitMatchesReviewedPatch(patch: string, cwd: string, commitMessage: string, parent: string, commit: string): void {
  const actualMessage = execFileSync("git", ["log", "-1", "--format=%s", commit], { cwd, encoding: "utf8" }).trim();
  if (actualMessage !== commitMessage) {
    throw new Error("existing HEAD is not the reviewed integration commit");
  }
  const indexDir = mkdtempSync(join(tmpdir(), "task-system-review-index-"));
  const indexPath = join(indexDir, "index");
  try {
    const env = { ...process.env, GIT_INDEX_FILE: indexPath };
    execFileSync("git", ["read-tree", parent], { cwd, env });
    execFileSync("git", ["apply", "--cached", "--3way", patch], { cwd, env, stdio: "pipe" });
    const reviewedTree = execFileSync("git", ["write-tree"], { cwd, env, encoding: "utf8" }).trim();
    const committedTree = execFileSync("git", ["rev-parse", `${commit}^{tree}`], { cwd, encoding: "utf8" }).trim();
    if (reviewedTree !== committedTree) throw new Error("existing HEAD is not the reviewed integration commit");
  } catch {
    throw new Error("existing HEAD is not the reviewed integration commit");
  } finally {
    rmSync(indexDir, { recursive: true, force: true });
  }
}

export function recoverReviewedPatch(patch: string, cwd: string, commitMessage: string): boolean {
  execFileSync("git", ["apply", "--reverse", "--check", patch], { cwd, stdio: "pipe" });
  const candidateFiles = parseApplyNumstatPaths(execFileSync("git", ["apply", "--numstat", "-z", patch], { cwd }));
  const dirtyFiles = parsePorcelainPaths(execFileSync("git", ["status", "--porcelain=v1", "-z"], { cwd }));
  const unrelated = [...dirtyFiles].filter((file) => !candidateFiles.has(file));
  if (unrelated.length) throw new Error(`integration recovery found unrelated repository changes: ${unrelated.join(", ")}`);
  if (!dirtyFiles.size) {
    const commits = execFileSync("git", ["log", "--format=%H", "--fixed-strings", `--grep=${commitMessage}`, "HEAD"], { cwd, encoding: "utf8" }).trim().split("\n").filter(Boolean);
    for (const commit of commits) {
      const parent = execFileSync("git", ["rev-parse", `${commit}^`], { cwd, encoding: "utf8" }).trim();
      try {
        assertCommitMatchesReviewedPatch(patch, cwd, commitMessage, parent, commit);
        return true;
      } catch {}
    }
    throw new Error("existing HEAD is not the reviewed integration commit");
  }
  execGitIndexWrite(["add", "-A", "--", ...candidateFiles], cwd);
  assertIndexMatchesReviewedPatch(patch, cwd);
  return false;
}

export function finalizeReviewedIntegration(options: { patch: string; cwd: string; commitMessage: string; integrated: boolean; checkpoint: () => void }): void {
  if (options.integrated) return;
  // Verification-only candidates (e.g. gate seams) deliver evidence reports, not
  // source changes: their reviewed patch is 0 bytes, git apply rejects empty
  // input, so integration is the checkpoint record itself with nothing to commit.
  if (statSync(options.patch).size === 0) {
    assertCleanGit(options.cwd);
    options.checkpoint();
    return;
  }
  const originalHead = execFileSync("git", ["rev-parse", "HEAD"], { cwd: options.cwd, encoding: "utf8" }).trim();
  let recovering = false;
  try {
    assertCleanGit(options.cwd);
    execFileSync("git", ["apply", "--index", "--check", options.patch], { cwd: options.cwd, stdio: "pipe" });
    execFileSync("git", ["apply", "--index", options.patch], { cwd: options.cwd, stdio: "pipe" });
  } catch (applyError) {
    try {
      execFileSync("git", ["apply", "--reverse", "--check", options.patch], { cwd: options.cwd, stdio: "pipe" });
      recovering = true;
    } catch {
      throw applyError;
    }
  }
  if (recovering && recoverReviewedPatch(options.patch, options.cwd, options.commitMessage)) {
    options.checkpoint();
    return;
  }
  assertIndexMatchesReviewedPatch(options.patch, options.cwd);
  try {
    execFileSync("git", ["diff", "--cached", "--quiet"], { cwd: options.cwd, stdio: "pipe" });
  } catch {
    const tree = execFileSync("git", ["write-tree"], { cwd: options.cwd, encoding: "utf8" }).trim();
    const commit = execFileSync("git", ["commit-tree", tree, "-p", originalHead, "-m", options.commitMessage], { cwd: options.cwd, encoding: "utf8" }).trim();
    assertCommitMatchesReviewedPatch(options.patch, options.cwd, options.commitMessage, originalHead, commit);
    execGitIndexWrite(["update-ref", "HEAD", commit, originalHead], options.cwd);
    assertCleanGit(options.cwd);
  }
  options.checkpoint();
}

export function assertCleanGit(cwd: string): void {
  const root = execFileSync("git", ["rev-parse", "--show-toplevel"], { cwd, encoding: "utf8" }).trim();
  const status = execFileSync("git", ["status", "--porcelain"], { cwd: root, encoding: "utf8" });
  if (status.trim()) throw new Error("Async pipeline requires a clean Git working tree. Commit or stash changes first.");
  const trackedState = execFileSync("git", ["ls-files", "--", ".pi/tasks.db", ".pi/tasks.db-shm", ".pi/tasks.db-wal", ".pi-subagents"], { cwd: root, encoding: "utf8" }).trim();
  if (trackedState) throw new Error("Async pipeline requires task DB and subagent artifacts to be untracked. Remove them from the Git index first.");
  const ignoredPaths = [".pi-subagents/probe"];
  if (existsSync(join(root, ".pi", "tasks.db"))) ignoredPaths.push(".pi/tasks.db");
  for (const path of ignoredPaths) {
    try {
      execFileSync("git", ["check-ignore", "-q", path], { cwd: root, stdio: "pipe" });
    } catch {
      throw new Error("Async pipeline requires .pi/tasks.db* and .pi-subagents/ to be ignored by Git.");
    }
  }
}

// Targeted re-review constraint: a follow-up review after a review-fix round
// answers exactly three questions (finding resolved, new defect in blast radius,

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function rejectedCandidatePatch(data: any, runs: PipelineRun[], cwd: string): string | undefined {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const activePack = (data.instruction_packs || []).find((pack: any) => pack.status === "active");
  const failedReview = currentFailedReview(runs, activePack);
  const candidate = failedReview?.candidate;
  if (!failedReview || !candidate || candidate.integrated_at) return undefined;
  if (!candidate?.integrated_patch_path || !existsSync(candidate.integrated_patch_path)) throw new Error("failed review candidate patch is unavailable");
  try {
    execFileSync("git", ["apply", "--check", candidate.integrated_patch_path], { cwd });
  } catch {
    try {
      execFileSync("git", ["apply", "--reverse", "--check", candidate.integrated_patch_path], { cwd, stdio: "pipe" });
      return undefined;
    } catch {
      // A sibling integration may have consumed only part of this candidate.
      // Keep the failed verdict as correction authority, but rebuild from current HEAD.
      return undefined;
    }
  }
  return candidate.integrated_patch_path;
}
