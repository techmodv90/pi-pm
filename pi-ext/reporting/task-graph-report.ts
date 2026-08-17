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

export function renderTaskGraphReportMarkdown(report: TaskGraphReport): string {
  const minutes = report.nodes.reduce((sum, node) => sum + node.estimated_effort_minutes, 0);
  return [
    "# TASK GRAPH",
    "",
    `Version ${report.version} | ${cell(report.execution_policy)} | ${report.nodes.length} nodes | ${minutes} estimated minutes`,
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