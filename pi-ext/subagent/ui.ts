import type { ExtensionAPI, ExtensionContext, Theme } from "@mariozechner/pi-coding-agent";
import { matchesKey, truncateToWidth, visibleWidth, type TUI } from "@mariozechner/pi-tui";

import { agentActivityLabel, agentRunTracker, formatAgentFooter, renderAgentWidget, type AgentRun } from "./tracker.ts";

export type AgentViewTab = "activity" | "prompt" | "output" | "details";
const tabs: AgentViewTab[] = ["activity", "prompt", "output", "details"];

function elapsed(run: AgentRun, now: number): string {
  const seconds = Math.max(0, Math.floor(((run.finishedAt || now) - run.startedAt) / 1000));
  return `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
}

function wrap(text: string, width: number): string[] {
  const lines: string[] = [];
  for (const source of text.split("\n")) {
    let line = source;
    while (line.length > width) {
      let split = line.lastIndexOf(" ", width);
      if (split < 1) split = width;
      lines.push(line.slice(0, split));
      line = line.slice(split).trimStart();
    }
    lines.push(line);
  }
  return lines;
}

export function renderAgentView(runs: AgentRun[], selected: number, tab: AgentViewTab, width: number, height: number, now = Date.now()): string[] {
  const maxWidth = Math.max(24, width);
  const line = (text: string) => truncateToWidth(text, maxWidth, "...");
  const output = [line("Agent Runs"), line("[Up/Down] select  [Tab] view  [x] stop  [Esc] close")];
  if (!runs.length) return [...output, "", "No agent runs in this session."];
  const safeSelected = Math.min(Math.max(selected, 0), runs.length - 1);
  const listStart = Math.max(0, Math.min(safeSelected - 2, runs.length - 5));
  for (const [offset, run] of runs.slice(listStart, listStart + 5).entries()) {
    const index = listStart + offset;
    const status = run.status === "running" ? run.lifecycleState || "starting" : run.status;
    output.push(line(`${index === safeSelected ? ">" : " "} ${status.padEnd(11)} ${run.agent} ${run.taskId || run.runId.slice(0, 8)} ${elapsed(run, now)}`));
  }
  const run = runs[safeSelected]!;
  output.push("", line(tabs.map((name) => name === tab ? `[${name}]` : ` ${name} `).join("  ")), "");
  let detail: string[];
  if (tab === "prompt") detail = wrap(run.task, maxWidth);
  else if (tab === "details") detail = [
    `Run: ${run.runId}`,
    `Agent: ${run.agent}`,
    `Task: ${run.taskId || "-"}`,
    `Stage: ${run.stage || "-"}`,
    `Model: ${run.model || "-"}`,
    `Status: ${run.status}`,
    `Activity: ${agentActivityLabel(run, now)}`,
    `Reason: ${run.terminalReason || "-"}`,
    `Cwd: ${run.cwd}`,
  ];
  else {
    const events = tab === "output" ? run.events.filter((event) => event.type === "message") : run.events;
    detail = events.map((event) => `${new Date(event.at).toLocaleTimeString()}  ${event.type.padEnd(11)} ${event.summary}`);
    if (!detail.length) detail = [tab === "output" ? "No assistant output yet." : "No activity yet."];
  }
  const available = Math.max(1, height - output.length);
  output.push(...detail.slice(-available).map(line));
  return output.slice(0, height);
}

class AgentRunsOverlay {
  private selected = 0;
  private tab = 0;
  private readonly unsubscribe: () => void;
  private readonly tui: TUI;
  private readonly theme: Theme;
  private readonly done: (runId?: string) => void;

  constructor(tui: TUI, theme: Theme, done: (runId?: string) => void) {
    this.tui = tui;
    this.theme = theme;
    this.done = done;
    this.unsubscribe = agentRunTracker.subscribe(() => this.tui.requestRender());
  }

  handleInput(data: string): void {
    const count = agentRunTracker.list().length;
    if (matchesKey(data, "escape") || matchesKey(data, "ctrl+c")) this.done();
    else if (matchesKey(data, "up")) this.selected = Math.max(0, this.selected - 1);
    else if (matchesKey(data, "down")) this.selected = Math.min(Math.max(0, count - 1), this.selected + 1);
    else if (matchesKey(data, "tab") || matchesKey(data, "right")) this.tab = (this.tab + 1) % tabs.length;
    else if (matchesKey(data, "left")) this.tab = (this.tab + tabs.length - 1) % tabs.length;
    else if (data === "x") this.done(agentRunTracker.list()[this.selected]?.runId);
    this.tui.requestRender();
  }

  render(width: number): string[] {
    const innerWidth = Math.max(1, width - 2);
    const content = renderAgentView(agentRunTracker.list(), this.selected, tabs[this.tab]!, innerWidth, 20);
    const fill = (line: string) => {
      const clipped = truncateToWidth(line, innerWidth, "...");
      return clipped + " ".repeat(Math.max(0, innerWidth - visibleWidth(clipped)));
    };
    const border = this.theme.fg("borderAccent", "+" + "-".repeat(innerWidth) + "+");
    return [
      this.theme.bg("selectedBg", border),
      ...content.map((line, index) => this.theme.bg("selectedBg", this.theme.fg("borderAccent", "|") + (index === 0 ? this.theme.fg("accent", this.theme.bold(fill(line))) : fill(line)) + this.theme.fg("borderAccent", "|"))),
      this.theme.bg("selectedBg", border),
    ];
  }

  invalidate(): void {}
  dispose(): void { this.unsubscribe(); }
}

function updateFooter(ctx: ExtensionContext): void {
  agentRunTracker.sync(ctx.cwd);
  const runs = agentRunTracker.list();
  const text = formatAgentFooter(runs, 120);
  ctx.ui.setStatus("task-agents", text ? ctx.ui.theme.fg("accent", text) : undefined);
  ctx.ui.setWidget("task-agents", renderAgentWidget(runs, 120), { placement: "aboveEditor" });
}

export function registerAgentTrackerUI(pi: ExtensionAPI): void {
  let context: ExtensionContext | undefined;
  let timer: NodeJS.Timeout | undefined;
  const refresh = () => { if (context) updateFooter(context); };
  agentRunTracker.subscribe(refresh);

  pi.on("session_start", async (_event, ctx) => {
    context = ctx;
    timer ??= setInterval(refresh, 1_000);
    timer.unref();
    refresh();
  });
  pi.on("session_shutdown", async () => {
    if (timer) clearInterval(timer);
    timer = undefined;
    context = undefined;
  });

  pi.registerCommand("agents", {
    description: "Inspect task-system subagent activity, prompts, output, and run details",
    handler: async (_args, ctx) => {
      const runId = await ctx.ui.custom<string | undefined>((tui, theme, _keybindings, done) => new AgentRunsOverlay(tui, theme, done), {
        overlay: true,
        overlayOptions: { width: "76%", minWidth: 64, maxHeight: 24, anchor: "center", margin: 2 },
      });
      if (!runId) return;
      const run = agentRunTracker.get(runId);
      if (run?.status !== "running") return ctx.ui.notify("Selected agent is no longer running.", "info");
      if (await ctx.ui.confirm("Stop agent", `Stop ${run.agent} (${run.taskId || run.runId.slice(0, 8)})?`)) agentRunTracker.stop(runId);
    },
  });
}