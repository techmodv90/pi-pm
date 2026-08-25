import { appendFileSync, existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { hostname } from "node:os";
import { join } from "node:path";
import type { SubagentUsage } from "./types.ts";

export type AgentRunStatus = "running" | "completed" | "failed" | "aborted";
export type AgentEventType = "started" | "message" | "tool" | "tool_result" | "stderr" | AgentRunStatus;
export type AgentLifecycleState = "starting" | "active" | "blocked" | "waiting" | "interrupted" | "stalled" | "finalizing" | "completed" | "failed";

export interface AgentRunEvent {
  at: number;
  type: AgentEventType;
  summary: string;
}

export interface AgentRun {
  runId: string;
  agent: string;
  task: string;
  cwd: string;
  stage?: string;
  taskId?: string;
  parentRunId?: string;
  model?: string;
  usage?: SubagentUsage;
  pid?: number;
  host?: string;
  processStartIdentity?: string;
  lifecycleState?: AgentLifecycleState;
  lifecycleDetail?: string;
  activityState?: string;
  activityAt?: number;
  heartbeatAt?: number;
  terminalReason?: string;
  status: AgentRunStatus;
  startedAt: number;
  finishedAt?: number;
  events: AgentRunEvent[];
  stop?: () => void;
}

export interface AgentRunStart {
  runId: string;
  agent: string;
  task: string;
  cwd: string;
  stage?: string;
  taskId?: string;
  parentRunId?: string;
  stop?: () => void;
}

type Listener = () => void;

export class AgentRunTracker {
  private readonly runs = new Map<string, AgentRun>();
  private readonly listeners = new Set<Listener>();

  start(input: AgentRunStart): AgentRun {
    const now = Date.now();
    const run: AgentRun = { ...input, parentRunId: input.parentRunId || process.env.PI_TASK_PARENT_RUN_ID, status: "running", lifecycleState: "starting", startedAt: now, heartbeatAt: now, events: [] };
    this.runs.set(run.runId, run);
    const dir = this.runDir(run);
    mkdirSync(dir, { recursive: true, mode: 0o700 });
    writeFileSync(join(dir, "prompt.txt"), run.task, { encoding: "utf8", mode: 0o600 });
    this.persist(run);
    this.event(run.runId, "started", run.agent);
    return run;
  }

  event(runId: string, type: AgentEventType, summary: string): void {
    const run = this.runs.get(runId);
    if (!run) return;
    const event = { at: Date.now(), type, summary: singleLine(summary).slice(0, 4000) };
    run.activityState = type === "tool" ? `using ${summary}` : type === "tool_result" ? "processing tool result" : type === "message" ? "thinking" : type === "stderr" ? "process reported an error" : type;
    run.activityAt = event.at;
    run.heartbeatAt = event.at;
    if (type === "tool" || type === "tool_result") {
      run.lifecycleState = "active";
      run.lifecycleDetail = type === "tool" ? `using ${summary}` : "processing tool result";
    } else if (type === "message") {
      run.lifecycleState = "waiting";
      run.lifecycleDetail = undefined;
    }
    run.events.push(event);
    if (run.events.length > 500) run.events.shift();
    // Cancelled runs may have their worktree (and run directory) removed while late
    // runner events still arrive; a vanished directory must not crash the host process.
    try {
      appendFileSync(join(this.runDir(run), "events.jsonl"), `${JSON.stringify(event)}\n`, { encoding: "utf8", mode: 0o600 });
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
    this.persist(run);
    this.emit();
  }

  finish(runId: string, status: Exclude<AgentRunStatus, "running">, summary = ""): void {
    const run = this.runs.get(runId);
    if (!run || run.status !== "running") return;
    run.status = status;
    run.lifecycleState = status === "completed" ? "completed" : "failed";
    run.lifecycleDetail = undefined;
    run.finishedAt = Date.now();
    run.activityState = status;
    run.activityAt = run.finishedAt;
    run.terminalReason = summary || status;
    this.event(runId, status, summary || status);
  }

  observeLifecycle(runId: string, state: AgentLifecycleState, detail?: string): void {
    const run = this.runs.get(runId);
    if (!run || run.status !== "running") return;
    run.lifecycleState = state;
    run.lifecycleDetail = detail;
    run.activityAt = Date.now();
    if (state !== "finalizing") run.heartbeatAt = run.activityAt;
    this.persist(run);
    this.emit();
  }

  setModel(runId: string, model?: string): void {
    const run = this.runs.get(runId);
    if (run && model) {
      run.model = model;
      this.persist(run);
    }
  }

  setPid(runId: string, pid?: number): void {
    const run = this.runs.get(runId);
    if (!run || !pid) return;
    run.pid = pid;
    run.host = hostname();
    run.processStartIdentity = processStartIdentity(pid);
    run.heartbeatAt = Date.now();
    this.persist(run);
  }

  setUsage(runId: string, usage: SubagentUsage): void {
    const run = this.runs.get(runId);
    if (!run) return;
    run.usage = { ...usage };
    this.persist(run);
    this.emit();
  }

  sync(cwd: string): void {
    const root = join(cwd, ".pi-subagents", "runs");
    if (!existsSync(root)) return;
    let changed = false;
    for (const entry of readdirSync(root, { withFileTypes: true })) {
      if (!entry.isDirectory()) continue;
      try {
        const snapshot = JSON.parse(readFileSync(join(root, entry.name, "state.json"), "utf8")) as AgentRun;
        if (!snapshot.parentRunId || !this.runs.has(snapshot.parentRunId)) continue;
        const current = this.runs.get(snapshot.runId);
        if (!current || current.events.length !== snapshot.events.length || current.status !== snapshot.status) {
          this.runs.set(snapshot.runId, snapshot);
          changed = true;
        }
      } catch {}
    }
    for (const run of this.runs.values()) {
      if (run.status !== "running" || !run.pid) continue;
      if (!isSameProcess(run)) this.finish(run.runId, "failed", "agent process exited without a terminal result");
    }
    if (changed) this.emit();
  }

  stop(runId: string): boolean {
    const run = this.runs.get(runId);
    if (!run || run.status !== "running" || !run.stop) return false;
    run.stop();
    return true;
  }

  get(runId: string): AgentRun | undefined {
    return this.runs.get(runId);
  }

  list(): AgentRun[] {
    return [...this.runs.values()].sort((a, b) => b.startedAt - a.startedAt);
  }

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private runDir(run: AgentRun): string {
    return join(run.cwd, ".pi-subagents", "runs", run.runId);
  }

  private persist(run: AgentRun): void {
    const { stop: _stop, ...snapshot } = run;
    try {
      writeFileSync(join(this.runDir(run), "state.json"), JSON.stringify(snapshot), { encoding: "utf8", mode: 0o600 });
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
  }

  private emit(): void {
    for (const listener of this.listeners) listener();
  }
}

function processStartIdentity(pid: number): string | undefined {
  try {
    return execFileSync("ps", ["-o", "lstart=", "-p", String(pid)], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim() || undefined;
  } catch { return undefined; }
}

function isSameProcess(run: AgentRun): boolean {
  try { process.kill(run.pid!, 0); } catch (error) { return (error as NodeJS.ErrnoException).code === "EPERM"; }
  if (!run.processStartIdentity || run.host !== hostname()) return true;
  return processStartIdentity(run.pid!) === run.processStartIdentity;
}

export const AGENT_STALL_AFTER_MS = 2 * 60 * 1000;

export function agentActivityLabel(run: AgentRun, now = Date.now()): string {
  if (run.status !== "running") return run.terminalReason || run.status;
  if (run.lifecycleState === "finalizing" || run.lifecycleState === "blocked" || run.lifecycleState === "waiting" || run.lifecycleState === "interrupted") return run.lifecycleState;
  if (run.heartbeatAt && now - run.heartbeatAt >= AGENT_STALL_AFTER_MS) return `stalled: no activity for ${Math.floor((now - run.heartbeatAt) / 1000)}s`;
  if (run.lifecycleState === "active") return run.lifecycleDetail ? `active · ${run.lifecycleDetail}` : "active";
  if (run.lifecycleState === "starting") return "starting";
  const last = run.events.at(-1);
  return run.activityState || (last?.type === "message" ? last.summary.split("\n")[0] : last?.type === "tool" ? `using ${last.summary}` : "thinking...");
}

export const agentRunTracker = new AgentRunTracker();

export function formatAgentFooter(runs: AgentRun[], _width: number, _now = Date.now()): string {
  const open = runs.filter((run) => run.status === "running");
  if (!open.length) return "";
  const active = open.filter((run) => run.lifecycleState === "active" || run.lifecycleState === "starting").length;
  return `${active} active · ${open.length} open`;
}

// TUI contract: one rendered line per string. Task text and child-process output
// contain newlines/control chars that would break pi-tui's diff-based redraw.
export function singleLine(text: string): string {
  return text.replace(/[\r\n\x00-\x1f\x7f]+/g, " ").trim();
}

export function renderAgentWidget(runs: AgentRun[], width: number, now = Date.now()): string[] {
  const active = runs.filter((run) => run.status === "running");
  if (!active.length) return [];
  const truncate = (value: string) => {
    const flat = singleLine(value);
    return flat.length <= width ? flat : `${flat.slice(0, Math.max(0, width - 3))}...`;
  };
  const elapsed = (run: AgentRun) => {
    const seconds = Math.max(0, Math.floor((now - run.startedAt) / 1000));
    return `${Math.floor(seconds / 60)}m${String(seconds % 60).padStart(2, "0")}s`;
  };
  const compact = (value: number) => {
    if (value < 1_000) return String(value);
    if (value < 1_000_000) return `${(value / 1_000).toFixed(value < 10_000 ? 1 : 0)}k`;
    return `${(value / 1_000_000).toFixed(1)}m`;
  };
  const usage = (run: AgentRun) => {
    const value = run.usage;
    return `↻ ${value?.turns || 0} · ${compact(value?.contextTokens || 0)} tok (i ${compact(value?.input || 0)}/o ${compact(value?.output || 0)}) · ${run.events.filter((event) => event.type === "tool").length} tools`;
  };
  const lines = ["● Agents"];
  const roots = active.filter((run) => !run.parentRunId || !active.some((candidate) => candidate.runId === run.parentRunId));
  const ordered = roots.flatMap((run) => [run, ...active.filter((candidate) => candidate.parentRunId === run.runId)]);
  ordered.forEach((run, index) => {
    const child = Boolean(run.parentRunId);
    const branch = index === ordered.length - 1 ? "└─" : "├─";
    const activity = agentActivityLabel(run, now);;
    lines.push(truncate(`${child ? "│  " : ""}${branch} · ${run.agent}  ${run.task || run.taskId || ""} · ${elapsed(run)} · ${usage(run)}`));
    lines.push(truncate(`${child ? "│  " : ""}${index === ordered.length - 1 ? "   " : "│  "}└ ${activity}`));
  });
  return lines;
}