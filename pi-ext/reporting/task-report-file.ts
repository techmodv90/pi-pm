import { mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join } from "node:path";
import { spawnSync } from "node:child_process";

/**
 * Build a temp HTML report output path for a task.
 * Expects task id and optional clock/temp root overrides; returns a unique file
 * path under the task-system report temp directory.
 */
export function buildTaskReportPath(taskId: string, now: Date = new Date(), tempRoot?: string): string {
  const root = tempRoot || tmpdir();
  const reportDir = join(root, "pi-task-system", "reports");
  const safeTaskId = sanitizeForFilename(taskId || "task");
  const stamp = now.toISOString().replaceAll(":", "-").replaceAll(".", "-");
  return join(reportDir, `${safeTaskId}-${stamp}.html`);
}

/**
 * Write task report HTML into temporary storage.
 * Expects task id, HTML content, and optional clock/temp-root overrides;
 * returns the written file path.
 */
export function writeTaskReportFile(taskId: string, html: string, now: Date = new Date(), tempRoot?: string): string {
  const filePath = buildTaskReportPath(taskId, now, tempRoot);
  mkdirSync(dirname(filePath), { recursive: true });
  writeFileSync(filePath, html, { encoding: "utf-8" });
  return filePath;
}

/**
 * Open a task report file using the platform default opener.
 * Expects an absolute or relative file path and returns success/error metadata
 * without throwing on open failures.
 */
export function openTaskReportFile(
  filePath: string,
  runner: (command: string, args: string[]) => { ok: true } | { ok: false; error: string } = runOpenCommand,
): { ok: true } | { ok: false; error: string } {
  try {
    const platform = process.platform;
    if (platform === "darwin") {
      return runner("open", [filePath]);
    }

    if (platform === "win32") {
      return runner("cmd", ["/c", "start", "", filePath]);
    }

    return runner("xdg-open", [filePath]);
  } catch (error: any) {
    return { ok: false, error: error?.message || String(error) };
  }
}

/**
 * Write and then open a task report HTML file.
 * Expects task id and HTML content; returns the output path and optional
 * open error when the file was written but opening failed.
 */
export function writeAndOpenTaskReport(taskId: string, html: string): { filePath: string; openError?: string } {
  const filePath = writeTaskReportFile(taskId, html);
  const openResult = openTaskReportFile(filePath);
  if (openResult.ok) return { filePath };
  return { filePath, openError: openResult.error };
}

/**
 * Execute an opener command and normalize its result shape.
 * Expects command binary and argv list; returns ok or an error message.
 */
function runOpenCommand(command: string, args: string[]): { ok: true } | { ok: false; error: string } {
  const result = spawnSync(command, args, { stdio: "ignore" });
  if (result.error) {
    return { ok: false, error: result.error.message };
  }

  if (typeof result.status === "number" && result.status !== 0) {
    return { ok: false, error: `${basename(command)} exited with code ${result.status}` };
  }

  return { ok: true };
}

/**
 * Sanitize values for safe cross-platform filename usage.
 * Expects a raw string and returns a filesystem-friendly token.
 */
function sanitizeForFilename(value: string): string {
  return value
    .trim()
    .replaceAll(/[^a-zA-Z0-9-_]+/g, "-")
    .replaceAll(/-+/g, "-")
    .replaceAll(/^-|-$/g, "") || "task";
}
