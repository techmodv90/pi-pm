import { execFile, execFileSync, execSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

export interface AutoCommitMessageContext {
  summary?: string;
  verificationStatus?: string;
  changedFiles?: string[];
}

export function execGitIndexWrite(args: string[], cwd: string): string {
  for (let attempt = 1; ; attempt++) {
    try {
      return execFileSync("git", args, { cwd, encoding: "utf-8", maxBuffer: 10 * 1024 * 1024 });
    } catch (error: any) {
      const message = `${error?.stderr || ""}\n${error?.message || ""}`;
      if (attempt >= 12 || !message.includes("index.lock")) throw error;
      Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 250);
    }
  }
}

export function withGitWriteLock<T>(cwd: string, operation: () => T): T {
  const commonDirRaw = execFileSync("git", ["rev-parse", "--git-common-dir"], { cwd, encoding: "utf-8" }).trim();
  const lockDir = resolve(cwd, commonDirRaw, "pi-task-system-write.lock");
  try {
    mkdirSync(lockDir);
  } catch (error: any) {
    if (error?.code !== "EEXIST") throw error;
    let owner: { pid?: number } = {};
    try { owner = JSON.parse(readFileSync(resolve(lockDir, "owner.json"), "utf8")); } catch {}
    if (owner.pid) {
      try {
        process.kill(owner.pid, 0);
        throw new Error(`repository Git write transaction is active in process ${owner.pid}`);
      } catch (livenessError: any) {
        if (livenessError?.code !== "ESRCH") throw livenessError;
      }
    }
    rmSync(lockDir, { recursive: true, force: true });
    mkdirSync(lockDir);
  }
  writeFileSync(resolve(lockDir, "owner.json"), JSON.stringify({ pid: process.pid, startedAt: new Date().toISOString() }), { mode: 0o600 });
  try {
    return operation();
  } finally {
    rmSync(lockDir, { recursive: true, force: true });
  }
}

function firstMeaningfulLine(text: string | undefined): string {
  return (text || "").split(/\r?\n/).map((line) => line.trim()).find(Boolean) || "";
}

function truncateCommitSubject(text: string, maxLength = 72): string {
  const normalized = text.replace(/\s+/g, " ").trim();
  if (normalized.length <= maxLength) return normalized;
  return `${normalized.slice(0, Math.max(0, maxLength - 1)).trimEnd()}…`;
}

/**
 * Build an evidence-oriented auto-commit message for task verification.
 * Expects task metadata plus optional verification summary/files and returns a
 * subject/body message that distinguishes what was verified, not only the title.
 */
export function buildAutoCommitMessage(taskTitle: string, taskId: string, context: AutoCommitMessageContext = {}): string {
  const status = context.verificationStatus || "verified";
  const headline = firstMeaningfulLine(context.summary) || taskTitle || "task verification";
  const subject = truncateCommitSubject(`verify(${taskId}): ${headline}`);
  const body = [
    `Task ID: ${taskId}`,
    `Task: ${taskTitle || "(untitled)"}`,
    `Verification status: ${status}`,
  ];
  if (context.summary) body.push("", "Summary:", context.summary.trim());
  if (context.changedFiles?.length) {
    body.push("", "Changed files:");
    for (const file of context.changedFiles.slice(0, 20)) body.push(`- ${file}`);
    if (context.changedFiles.length > 20) body.push(`- …and ${context.changedFiles.length - 20} more`);
  }
  return `${subject}\n\n${body.join("\n")}`;
}

/** Resolve the Go pic CLI binary path. */
export function findPicCli(): string {
  const configured = process.env.PIC_CLI || process.env.PI_TASK_SYSTEM_PIC;
  if (configured && existsSync(configured)) return configured;

  const extDir = dirname(fileURLToPath(import.meta.url));
  const realExtDir = realpathSync(extDir);
  const binaryName = process.platform === "win32" ? "pic.exe" : "pic";
  const candidates = [
    resolve(realExtDir, "..", "..", "go-pic", "dist", binaryName),
    resolve(process.env.HOME || "~", ".pi", "task-system", "go-pic", "dist", binaryName),
    resolve(process.env.HOME || "~", ".pi", "bin", binaryName),
  ];
  return candidates.find(existsSync) || "pic";
}

/** Execute the Go pic CLI and parse JSON output. */
export function execPic(args: string[], cwd: string): any {
  const picPath = findPicCli();

  try {
    const output = execFileSync(picPath, args, { cwd, encoding: "utf-8", maxBuffer: 10 * 1024 * 1024, stdio: ["ignore", "pipe", "pipe"] });
    return JSON.parse(output.trim());
  } catch (e: any) {
    if (e.stderr) {
      try { return JSON.parse(e.stderr.toString()); } catch {}
    }
    if (e.stdout) {
      try { return JSON.parse(e.stdout.toString()); } catch {}
    }
    return { error: e.message || "Failed to execute pic CLI" };
  }
}

/** Execute the Go pic CLI without blocking Pi's event loop. */
export function execPicAsync(args: string[], cwd: string): Promise<any> {
  return new Promise((resolveResult) => {
    execFile(findPicCli(), args, { cwd, encoding: "utf-8", maxBuffer: 10 * 1024 * 1024 }, (error, stdout, stderr) => {
      for (const text of [stdout, stderr]) {
        if (!text) continue;
        try {
          resolveResult(JSON.parse(text.trim()));
          return;
        } catch {}
      }
      resolveResult(error ? { error: error.message } : {});
    });
  });
}

/** Execute the Go pic CLI and return text output. */
export function execPicText(args: string[], cwd: string): string {
  return execFileSync(findPicCli(), args, { cwd, encoding: "utf-8", maxBuffer: 10 * 1024 * 1024, stdio: ["ignore", "pipe", "pipe"] });
}

/** Check if a task database exists in cwd or ancestor directories */
export function hasDb(cwd: string): boolean {
  let dir = resolve(cwd);
  while (true) {
    if (existsSync(resolve(dir, ".pi", "tasks.db"))) return true;
    const parent = dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return false;
}

/** Get list of changed files for review context */
export function getChangedFiles(cwd: string): string[] {
  try {
    const tracked = execSync("git diff --name-only HEAD", { cwd, encoding: "utf-8", maxBuffer: 10 * 1024 * 1024 })
      .trim()
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    const untracked = execSync("git ls-files --others --exclude-standard", { cwd, encoding: "utf-8", maxBuffer: 10 * 1024 * 1024 })
      .trim()
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    return Array.from(new Set([...tracked, ...untracked])).slice(0, 100);
  } catch {
    return [];
  }
}

/**
 * Auto-commit changes for a completed task.
 * Stages all changed files and creates a commit with task info.
 * Returns { success: boolean, message: string, commitHash?: string }.
 */
export function autoCommitTask(taskTitle: string, taskId: string, cwd: string, context: AutoCommitMessageContext = {}): { success: boolean; message: string; commitHash?: string } {
  try {
    // Check if we're in a git repo
    execSync("git rev-parse --git-dir", { cwd, encoding: "utf-8", stdio: "pipe" });
  } catch {
    return { success: false, message: "Not a git repository" };
  }

  try {
    // Check if there are any changes to commit
    const hasChanges = execSync("git status --porcelain", { cwd, encoding: "utf-8", maxBuffer: 10 * 1024 * 1024 }).trim();
    if (!hasChanges) {
      return { success: false, message: "No changes to commit" };
    }

    const changedFiles = getChangedFiles(cwd);

    // Stage all changes
    execGitIndexWrite(["add", "-A"], cwd);

    // Create evidence-oriented commit message
    const commitMsg = buildAutoCommitMessage(taskTitle, taskId, { ...context, changedFiles });

    // Commit
    const result = execGitIndexWrite(["commit", "-m", commitMsg], cwd);
    const hashMatch = result.match(/\[.+ ([0-9a-f]{7,})\]/);
    const commitHash = hashMatch ? hashMatch[1] : undefined;

    return { success: true, message: `Committed: ${commitHash || 'changes'}`, commitHash };
  } catch (e: any) {
    const stderr = e.stderr?.toString() || e.message || "Unknown error";
    return { success: false, message: `Commit failed: ${stderr.trim()}` };
  }
}

/**
 * Resolve the current git HEAD commit hash.
 * Expects a git repository cwd and returns the full hash or an empty string when
 * no commit can be resolved.
 */
export function getHeadCommitHash(cwd: string): string {
  try {
    return execSync("git rev-parse HEAD", { cwd, encoding: "utf-8", stdio: "pipe" }).trim();
  } catch {
    return "";
  }
}
