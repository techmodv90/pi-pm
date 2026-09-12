export type AgentSource = "user" | "project" | "packaged" | "unknown";

export interface AgentConfig {
  name: string;
  description: string;
  tools?: string[];
  skills?: string[];
  model?: string;
  thinking?: string;
  systemPrompt: string;
  source: AgentSource;
  filePath: string;
}

export interface SubagentUsage {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  contextTokens: number;
  turns: number;
}

export interface SubagentWorkspace {
  assignedWorktree: string;
  childProcessCwd: string;
  bashCwd: string;
  readToolRoot: string;
  editToolRoot: string;
  writeToolRoot: string;
  applyPatchRoot: string;
  gitToplevel: string;
  head: string;
  statusBefore: string;
  statusAfter: string;
  diffStatAfter: string;
  changedFiles?: string[];
  generatedFiles?: string[];
}

export interface SubagentResult {
  runId: string;
  agent: string;
  task: string;
  exitCode: number;
  messages: any[];
  stderr: string;
  usage: SubagentUsage;
  model?: string;
  stopReason?: string;
  errorMessage?: string;
  /** Stable classification tag for transient provider faults surfaced after in-claim retries exhaust. */
  failureCode?: string;
  workspace?: SubagentWorkspace;
}

import type { AgentRunTracker } from "./tracker.ts";
import type { HerdrPanel } from "./herdr-panel.ts";

export interface SubagentSpec {
  agent: AgentConfig;
  task: string;
  cwd: string;
  acceptance?: "checked" | "attested";
  isolation?: "worktree";
  signal?: AbortSignal;
  runId?: string;
  stage?: string;
  taskId?: string;
  tracker?: AgentRunTracker;
  skillDirectories?: string[];
  skillFamilies?: string[];
  herdrPanel?: HerdrPanel;
  initialPatchPath?: string;
  preparedWorktree?: string;
  /** Host-side pi session file (create-or-continue) pinned outside the worktree; enables review-fix resume. */
  sessionPath?: string;
  /** Backoff base (ms) between in-claim transient-provider retries; defaults to RUNNER_TRANSIENT_BACKOFF_MS. */
  transientBackoffMs?: number;
  /** Durable worker worktree constraint (RLB-GAP-001): when set, the worktree is keyed by this id (instruction pack) and retained across report-less transient failures instead of destroyed per attempt. */
  durableWorktreeKey?: string;
  /** Resume preamble for a retained pack worktree, prepended to the task on every launch. */
  resumeFailureMode?: string;
}

export interface SubagentHandle {
  id: string;
  pid?: number;
  result: Promise<SubagentResult>;
  stop(): void;
}

export interface SubagentUpdate {
  result: SubagentResult;
  event: "message" | "tool_result";
}