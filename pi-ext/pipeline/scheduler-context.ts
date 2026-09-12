import type { PicShowDocument } from "./pic-show.ts";
import type { PipelineRun } from "./pipeline-types.ts";
import type { EphemeralHandoffStore } from "../core/ephemeral-handoffs.ts";

/** Facade the split scheduler operation modules use instead of `this`. */
export interface SchedulerDeps {
  readonly cwd: string;
  /** Fail-closed typed view of one `pic show` document. */
  showItem(id: string): PicShowDocument;
  pipelineRuns(taskId: string): PipelineRun[];
  /** Owner-facing follow-up message delivery. */
  sendUserMessage(text: string): void;
  notifyBlockedAttempt(run: PipelineRun, reason: string): void;
  addRoot(id: string): void;
  /** Durable worker worktree constraint (RLB-GAP-001): failure modes of retained pack worktrees keyed by pack id. */
  retainedFailures: Map<string, string>;
  /** Ephemeral planning-handoff store (never persisted). */
  handoffs: EphemeralHandoffStore;
}
