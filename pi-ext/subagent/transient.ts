import type { SubagentResult, SubagentSpec } from "./types.ts";
import { finalAssistantText } from "./spawn-utils.ts";

// Transient provider fault classification constraint: the runner classifies
// output BEFORE any downstream XML validation so empty-output and inference-
// abort faults are retried inside the same claim instead of being surfaced as
// malformed-verdict failures. A completed agent that returned assistant text is
// never transient.
export type RunnerTransientFault = "empty_output" | "inference_abort" | "provider_stall" | "none";

export const RUNNER_TRANSIENT_RETRIES = 2;
export const RUNNER_TRANSIENT_BACKOFF_MS = 500;

export function classifyRunnerTransientFault(result: SubagentResult): RunnerTransientFault {
  if (result.stopReason === "aborted") return "none";
  // Watchdog kill means the provider wedged mid-run: same transient class as an
  // inference abort, retried in-claim rather than burning a numbered attempt.
  if (result.stopReason === "stalled") return "provider_stall";
  // Deadline kill is a throughput fault, not a candidate defect: the child was
  // working when the process timer fired, so it retries in-claim with a fresh
  // timer (SQ-1: fresh deadline budget per resume) instead of being consumed as
  // a deterministic numbered attempt.
  if (result.stopReason === "timed_out") return "provider_stall";
  const diagnostic = `${result.errorMessage || ""}\n${result.stderr || ""}`;
  if (/inference[\s_-]?abort|inference\s+error|empty[\s_-]?(?:model|provider)[\s_-]?(?:output|response)|provider[\s_-]?error/i.test(diagnostic)) return "inference_abort";
  // Empty assistant output is transient independently of exit code: a provider
  // that returns no text (even with a nonzero child exit and no matching
  // diagnostic) must still be retried in-claim rather than misclassified as a
  // normal failure. Explicit user aborts are excluded above.
  if (!finalAssistantText(result.messages || []).trim()) return "empty_output";
  return "none";
}

export function runnerTransientBackoffMs(retryIndex: number, spec: SubagentSpec): number {
  const base = spec.transientBackoffMs !== undefined ? spec.transientBackoffMs : RUNNER_TRANSIENT_BACKOFF_MS;
  return base * Math.max(1, retryIndex);
}
