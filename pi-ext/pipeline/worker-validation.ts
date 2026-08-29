export const DEFAULT_GENERATED_FILES = ["test-results/**", "playwright-report/**", "coverage/**", ".nyc_output/**"];
import { join, matchesGlob } from "node:path";
import { existsSync, statSync } from "node:fs";
import type { PipelineRun } from "./pipeline-types.ts";

export function workerPatch(run: PipelineRun): string {
  return join(run.async_dir || "", "worktree-diffs", `task-${run.child_index || 0}-task-worker.patch`);
}

export function validateWorkerPatchArtifact(patchPath: string, outputPath: string, report: any): void {
  if (!existsSync(patchPath)) throw new Error(`worker patch missing: ${patchPath}; output: ${outputPath}`);
  if (statSync(patchPath).size === 0 && Array.isArray(report.changedFiles) && report.changedFiles.length > 0) {
    throw new Error(`worker artifact invalid: completion report claims changed files but patch is empty; output: ${outputPath}; patch: ${patchPath}`);
  }
}

export function scopeCovers(path: string, patterns: string[]): boolean {
  return patterns.some((pattern) => pattern === "." || path === pattern || path.startsWith(`${pattern.replace(/\/+$/, "")}/`) || matchesGlob(path, pattern));
}

export function validateWorkerChangedFiles(changedFiles: string[], constraints: any): { unexpected: string[] } {
  const protectedPaths = [".git/**", ".pi/**", ".pi-subagents/**", ...(Array.isArray(constraints?.protected_paths) ? constraints.protected_paths : [])];
  const protectedChanges = changedFiles.filter((path) => scopeCovers(path, protectedPaths));
  if (protectedChanges.length) throw new Error(`worker changed protected path: ${protectedChanges.join(", ")}`);
  const roots = Array.isArray(constraints?.scope_roots) ? constraints.scope_roots : [];
  const approvalRequired = Array.isArray(constraints?.approval_required) ? constraints.approval_required : [];
  return { unexpected: [...new Set(changedFiles.filter((path) => (roots.length > 0 && !scopeCovers(path, roots)) || scopeCovers(path, approvalRequired)))] };
}

export function validateWorkerOutput(status: "done" | "partial" | "blocked", actualChangedFiles: string[], constraints: any): { reported: string[]; actual: string[]; mismatch: boolean; unexpected?: string[] } {
  if (status !== "done") return { reported: [], actual: [], mismatch: false };
  const actual = [...new Set<string>(actualChangedFiles)].sort();
  const scope = validateWorkerChangedFiles(actual, constraints);
  const result: { reported: string[]; actual: string[]; mismatch: boolean; unexpected?: string[] } = { reported: actual, actual, mismatch: false };
  if (scope.unexpected.length) result.unexpected = scope.unexpected;
  return result;
}

export function filterGeneratedFiles(paths: string[], constraints: any): { changedFiles: string[]; generatedFiles: string[] } {
  const patterns = [...DEFAULT_GENERATED_FILES, ...(Array.isArray(constraints?.generated_files) ? constraints.generated_files : [])];
  const generatedFiles = paths.filter((path) => patterns.some((pattern) => {
    const recursiveRoot = pattern.endsWith("/**") ? pattern.slice(0, -3).replace(/\/+$/, "") : "";
    return (recursiveRoot && (path === recursiveRoot || path.startsWith(`${recursiveRoot}/`))) || matchesGlob(path, pattern);
  }));
  return { changedFiles: paths.filter((path) => !generatedFiles.includes(path)), generatedFiles };
}

export function pipelineFailureResult(reason: string): Record<string, string> {
  if (reason.includes("transient provider fault") || reason.includes("inference") || reason.includes("empty_output") || reason.includes("inference_abort")) return { failure_code: "transient_provider" };
  if (reason.includes("worker artifact invalid") || reason.includes("worker patch missing")) return { failure_code: "worker_artifact_invalid" };
  if (reason.includes("autofix made no repository changes")) return { failure_code: "no_progress_autofix" };
  if (reason.includes("Agent is already processing") || reason.includes("stale or invalid lease") || reason.includes("lease expired") || reason.includes("pipeline bind rejected")) return { failure_code: "runner_protocol_invalid" };
  if (reason.includes("completion report missing")) return { failure_code: "runner_protocol_invalid" };
  if (reason.includes("required verification did not pass") && /docker|postgres|TEST_DATABASE_URL|database|connection refused|connection reset/i.test(reason)) return { failure_code: "environment_blocked" };
  if (reason.includes("worker output") || reason.includes("required verification did not pass") || reason.includes("unsatisfied criteria") || reason.includes("changedFiles do not match")) return { failure_code: "worker_output_invalid" };
  return {};
}
