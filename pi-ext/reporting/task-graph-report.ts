export interface TaskGraphNode {
  key: string;
  type?: string;
  parent_key?: string;
  name: string;
  goal: string;
  requirement_keys: string[];
  depends_on: string[];
  priority: string;
  module: string;
  estimated_effort_minutes: number;
  files: string[];
  verification: unknown[];
  skillFamilies: string[];
  [key: string]: unknown;
}

export interface TaskGraphReport {
  version: number;
  execution_policy: string;
  nodes: TaskGraphNode[];
}

const text = (value: unknown, field: string): string => {
  if (typeof value !== "string" || !value.trim()) throw new Error(`Task Graph ${field} is required`);
  return value;
};

export function parseTaskGraphReportJson(content: string): TaskGraphReport {
  const report = JSON.parse(content) as Partial<TaskGraphReport>;
  if (report.version !== 3) throw new Error("Task Graph version must be 3");
  text(report.execution_policy, "execution_policy");
  if (!Array.isArray(report.nodes) || !report.nodes.length) throw new Error("Task Graph nodes are required");
  const keys = new Set<string>();
  for (const node of report.nodes) {
    text(node.key, "node key");
    if (keys.has(node.key)) throw new Error(`Task Graph duplicate node key ${node.key}`);
    keys.add(node.key);
    text(node.name, `${node.key} name`);
    text(node.goal, `${node.key} goal`);
    text(node.priority, `${node.key} priority`);
    text(node.module, `${node.key} module`);
    if (!node.requirement_keys?.length) throw new Error(`Task Graph ${node.key} requirement_keys are required`);
    if (!Array.isArray(node.depends_on) || !Array.isArray(node.files) || !Array.isArray(node.verification) || !Array.isArray(node.skillFamilies)) throw new Error(`Task Graph ${node.key} arrays are incomplete`);
    if (!Number.isFinite(node.estimated_effort_minutes) || node.estimated_effort_minutes < 1) throw new Error(`Task Graph ${node.key} estimated_effort_minutes is invalid`);
  }
  const visiting = new Set<string>();
  const visited = new Set<string>();
  const byKey = new Map(report.nodes.map((node) => [node.key, node]));
  const visit = (key: string) => {
    if (visiting.has(key)) throw new Error(`Task Graph dependency cycle at ${key}`);
    if (visited.has(key)) return;
    visiting.add(key);
    for (const dependency of byKey.get(key)!.depends_on) {
      if (!byKey.has(dependency) || dependency === key) throw new Error(`Task Graph ${key} has invalid dependency ${dependency}`);
      visit(dependency);
    }
    visiting.delete(key);
    visited.add(key);
  };
  for (const key of keys) visit(key);
  return report as TaskGraphReport;
}

const cell = (value: unknown) => String(value ?? "").replaceAll("|", "\\|").replaceAll("\n", " ");
const mermaid = (value: unknown) => String(value ?? "").replaceAll('"', "'").replaceAll("\n", " ");

function graphPresentation(report: TaskGraphReport): { diagram: string[]; waves: TaskGraphNode[][]; criticalPath: TaskGraphNode[] } {
  const byKey = new Map(report.nodes.map((node) => [node.key, node]));
  const index = new Map(report.nodes.map((node, position) => [node.key, `N${position}`]));
  const levels = new Map<string, number>();
  const durations = new Map<string, number>();
  const predecessors = new Map<string, string>();
  const visit = (node: TaskGraphNode): number => {
    if (levels.has(node.key)) return levels.get(node.key)!;
    let level = 0;
    let duration = node.estimated_effort_minutes;
    for (const dependency of node.depends_on) {
      const dependencyNode = byKey.get(dependency)!;
      level = Math.max(level, visit(dependencyNode) + 1);
      const candidate = durations.get(dependency)! + node.estimated_effort_minutes;
      if (candidate > duration) {
        duration = candidate;
        predecessors.set(node.key, dependency);
      }
    }
    levels.set(node.key, level);
    durations.set(node.key, duration);
    return level;
  };
  report.nodes.forEach(visit);
  const waves: TaskGraphNode[][] = [];
  for (const node of report.nodes) (waves[levels.get(node.key)!] ||= []).push(node);
  const terminal = report.nodes.reduce((longest, node) => durations.get(node.key)! > durations.get(longest.key)! ? node : longest);
  const criticalPath: TaskGraphNode[] = [];
  for (let key: string | undefined = terminal.key; key; key = predecessors.get(key)) criticalPath.unshift(byKey.get(key)!);
  return {
    diagram: [
      "```mermaid",
      "flowchart TD",
      ...report.nodes.map((node) => `    ${index.get(node.key)}["${mermaid(node.key)}: ${mermaid(node.name)}"]`),
      ...report.nodes.flatMap((node) => node.depends_on.map((dependency) => `    ${index.get(dependency)} --> ${index.get(node.key)}`)),
      "```",
    ],
    waves,
    criticalPath,
  };
}

export function renderTaskGraphReportMarkdown(report: TaskGraphReport): string {
  const minutes = report.nodes.reduce((sum, node) => sum + node.estimated_effort_minutes, 0);
  const graph = graphPresentation(report);
  return [
    "# TASK GRAPH",
    "",
    `Version ${report.version} | ${cell(report.execution_policy)} | ${report.nodes.length} nodes | ${minutes} estimated minutes`,
    "",
    "## DEPENDENCY DIAGRAM",
    "",
    ...graph.diagram,
    "",
    "## PARALLEL EXECUTION WAVES",
    "",
    "| Wave | Parallel Work | Starts When |",
    "|------|---------------|-------------|",
    ...graph.waves.map((wave, index) => `| ${index + 1} | ${wave.map((node) => `\`${cell(node.key)}\`: ${cell(node.name)}`).join("; ")} | ${index === 0 ? "Immediately" : `Wave ${index} completes`} |`),
    "",
    `**Critical path:** ${graph.criticalPath.map((node) => `\`${cell(node.key)}\``).join(" -> ")}`,
    "",
    "| Key | Type | Task | Module | Requirements | Depends On | Effort |",
    "|-----|------|------|--------|--------------|------------|--------|",
    ...report.nodes.map((node) => `| ${cell(node.key)} | ${cell(node.type || "task")} | ${cell(node.name)} | ${cell(node.module)} | ${node.requirement_keys.map(cell).join(", ")} | ${node.depends_on.map(cell).join(", ") || "None"} | ${node.estimated_effort_minutes} min |`),
    "",
    "## NODE CONTRACTS",
    ...report.nodes.flatMap((node) => [
      "",
      `### ${cell(node.key)}: ${cell(node.name)}`,
      node.goal,
      `- **Files:** ${node.files.map(cell).join(", ") || "None"}`,
      `- **Verification:** ${node.verification.map((entry) => cell(typeof entry === "string" ? entry : JSON.stringify(entry))).join("; ") || "None"}`,
      `- **Skill families:** ${node.skillFamilies.map(cell).join(", ") || "None"}`,
    ]),
  ].join("\n");
}