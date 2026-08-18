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
  workspace?: SubagentWorkspace;
}

export interface SubagentUpdate {
  result: SubagentResult;
  event: "message" | "tool_result";
}