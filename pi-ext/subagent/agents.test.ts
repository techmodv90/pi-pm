import assert from "node:assert/strict";
import test from "node:test";
import { discoverAgents, parseFrontmatter, packagedAgentsDir } from "./agents.ts";

test("parseFrontmatter separates agent metadata from its prompt", () => {
  const parsed = parseFrontmatter("---\nname: worker\ntools: read, bash\n---\nDo work.");
  assert.deepEqual(parsed.frontmatter, { name: "worker", tools: "read, bash" });
  assert.equal(parsed.body, "Do work.");
});

test("discoverAgents loads packaged task-system agents", () => {
  const agents = discoverAgents(process.cwd(), "project");
  const scout = agents.find((agent) => agent.name === "task-scout");
  assert.deepEqual(scout?.skills, ["codanna-explore", "codanna-review"]);
  assert.equal(scout?.tools?.includes("research"), false);
  assert.deepEqual(agents.find((agent) => agent.name === "task-worker")?.skills, ["test-first", "verification-gate", "testing-anti-patterns", "ponytail"]);

  assert.deepEqual(agents.find((agent) => agent.name === "task-reviewer")?.skills, ["defense-in-depth"]);
  assert.match(packagedAgentsDir(), /pi-ext[\\/]agents$/);
});

test("packaged read-only task agents do not receive mutation tools", () => {
  const agents = discoverAgents(process.cwd(), "project");
  for (const name of ["task-scout", "task-reviewer"]) {
    const tools = agents.find((agent) => agent.name === name)?.tools || [];
    assert.equal(tools.includes("write"), false, `${name} must not receive write`);
    assert.equal(tools.includes("edit"), false, `${name} must not receive edit`);
  }
});