import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import type { AgentConfig, AgentSource } from "./types.ts";

export type AgentScope = "user" | "project" | "both";

export function parseFrontmatter(content: string): { frontmatter: Record<string, string>; body: string } {
  const match = content.match(/^---\s*\n([\s\S]*?)\n---\s*\n?([\s\S]*)$/);
  if (!match) return { frontmatter: {}, body: content };
  const frontmatter: Record<string, string> = {};
  for (const line of match[1].split(/\r?\n/)) {
    const separator = line.indexOf(":");
    if (separator < 0) continue;
    const key = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim().replace(/^['"]|['"]$/g, "");
    if (key && value) frontmatter[key] = value;
  }
  return { frontmatter, body: match[2] };
}

function loadAgentsFromDir(dir: string, source: AgentSource): AgentConfig[] {
  if (!existsSync(dir)) return [];
  const agents: AgentConfig[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (!entry.name.endsWith(".md") || (!entry.isFile() && !entry.isSymbolicLink())) continue;
    const filePath = join(dir, entry.name);
    try {
      const { frontmatter, body } = parseFrontmatter(readFileSync(filePath, "utf8"));
      if (!frontmatter.name || !frontmatter.description) continue;
      agents.push({
        name: frontmatter.name,
        description: frontmatter.description,
        tools: frontmatter.tools?.split(",").map((tool) => tool.trim()).filter(Boolean),
        skills: frontmatter.skills?.split(",").map((skill) => skill.trim()).filter(Boolean),
        model: frontmatter.model,
        thinking: frontmatter.thinking,
        systemPrompt: body,
        source,
        filePath,
      });
    } catch {
      // Ignore unreadable or malformed agent files, matching upstream discovery behavior.
    }
  }
  return agents;
}

function nearestProjectAgentsDir(cwd: string): string | null {
  let current = resolve(cwd);
  while (true) {
    const candidate = join(current, ".pi", "agents");
    if (existsSync(candidate) && statSync(candidate).isDirectory()) return candidate;
    const parent = dirname(current);
    if (parent === current) return null;
    current = parent;
  }
}

export function packagedAgentsDir(): string {
  return fileURLToPath(new URL("../agents", import.meta.url));
}

export function discoverAgents(cwd: string, scope: AgentScope = "both"): AgentConfig[] {
  const dirs: Array<[string, AgentSource]> = [[packagedAgentsDir(), "packaged"]];
  if (scope !== "project") dirs.push([join(homedir(), ".pi", "agent", "agents"), "user"]);
  if (scope !== "user") {
    const project = nearestProjectAgentsDir(cwd);
    if (project) dirs.push([project, "project"]);
  }
  const agents = new Map<string, AgentConfig>();
  for (const [dir, source] of dirs) for (const agent of loadAgentsFromDir(dir, source)) agents.set(agent.name, agent);
  return [...agents.values()];
}