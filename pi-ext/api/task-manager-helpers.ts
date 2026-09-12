import { execFileSync } from "node:child_process";
import { execPic } from "../core/cli-helpers.ts";
import { latestRriTScenarios } from "../tasking/work-item-prompts.ts";
import type { RriDraftLineage } from "../core/rri-drafts.ts";

export function aggregateGitEvidence(cwd: string): { branch: string; head: string; baseBranch: string; baseCommit: string } {
  const git = (...args: string[]) => execFileSync("git", args, { cwd, encoding: "utf8" }).trim();
  const branch = git("branch", "--show-current");
  if (!branch) throw new Error("aggregate delivery requires a named Git branch");
  const baseBranch = "develop";
  let baseCommit = "";
  try { baseCommit = git("rev-parse", `refs/remotes/origin/${baseBranch}`); }
  catch { baseCommit = git("rev-parse", baseBranch); }
  return { branch, head: git("rev-parse", "HEAD"), baseBranch, baseCommit };
}

export function approvedScanLineage(cwd: string, workItemId: string): RriDraftLineage {
  const data = execPic(["show", workItemId], cwd);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split tool)
  const checkpoint = (data.checkpoints || []).find((entry: any) => entry.stage === "scan");
  if (!checkpoint?.artifact_id || !checkpoint?.content_hash) throw new Error("RRI interview requires an approved Scan checkpoint");
  return { artifactId: checkpoint.artifact_id, contentHash: checkpoint.content_hash };
}

export function rriDraftRoot(cwd: string): string {
  const project = execPic(["project", "current"], cwd);
  if (!project.root_path) throw new Error(project.error || "RRI interview requires a current project root");
  return project.root_path;
}

// RRI-T scenario identity constraint: the id-based identity
// (dimension|stress_axis|requirement_id|id) — shared with the canonical Go
// validator and the authoring merge — never the persona, so two persisted
// scenarios may share persona, dimension, stress axis, and requirement while
// remaining distinct by id, and one persisted scenario can be deferred at most
// once.
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split tool)
function rriTScenarioIdentity(scenario: any): string {
  return `${scenario.dimension}|${scenario.stress_axis}|${scenario.requirement_id}|${scenario.id}`;
}

// RRI-T save-before-execution (OB-5) keeps its artifact persistence, but the
// scenario authoring is in-session contractor methodology work (see
// /apm review): no persona subagents are spawned for authoring, and grading
// below fails closed when the persisted artifact is missing.

// RRI-T contractor grading (OB-6/OB-7): compile the submission from the
// persisted scenario artifact and the contractor's in-session evidence; each
// retained scenario receives exactly one outcome (PASS/ACCEPTABLE/PAINFUL/FAIL
// with evidence, or not_applicable with a reason that stays out of the
// executable scenarios), and FAIL blocking plus PAINFUL remediation/deferral
// remain authoritative on the Go validation side.
// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split tool)
export function compileRriTSubmission(data: any, gradedJson: string): string {
  const persisted = latestRriTScenarios(data);
  if (!persisted) throw new Error("persisted rri_t_scenarios artifact is missing; authoring was never saved, so aggregate verification is blocked instead of executing an unpersisted list");
  let graded: { scenarios?: any[]; not_applicable?: any[] };
  try {
    graded = JSON.parse(gradedJson || "");
  } catch {
    throw new Error("rri_t_evidence_json must be a JSON object with scenarios and/or not_applicable arrays");
  }
  if (!graded || typeof graded !== "object" || Array.isArray(graded) || (!Array.isArray(graded.scenarios) && !Array.isArray(graded.not_applicable))) {
    throw new Error("rri_t_evidence_json must be a JSON object with scenarios and/or not_applicable arrays");
  }
  const persistedByIdentity = new Map<string, any>();
  for (const scenario of persisted.content.scenarios || []) {
    const key = rriTScenarioIdentity(scenario);
    if (!persistedByIdentity.has(key)) persistedByIdentity.set(key, scenario);
  }
  const outcomes = new Set<string>();
  const scenarios: any[] = [];
  for (const grade of graded.scenarios || []) {
    const key = rriTScenarioIdentity(grade);
    const match = persistedByIdentity.get(key);
    if (!match) throw new Error(`graded scenario ${key} is not in the persisted rri_t_scenarios artifact`);
    if (outcomes.has(key)) throw new Error(`scenario ${key} received more than one outcome`);
    outcomes.add(key);
    if (String(grade.procedure || "").trim() !== String(match.procedure || "").trim()) throw new Error(`graded scenario ${key} must reuse the persisted procedure verbatim`);
    if (!String(grade.evidence || "").trim()) throw new Error(`graded scenario ${key} requires executed evidence`);
    if (!["PASS", "ACCEPTABLE", "PAINFUL", "FAIL"].includes(String(grade.result || ""))) throw new Error(`graded scenario ${key} result must be PASS, ACCEPTABLE, PAINFUL, or FAIL`);
    scenarios.push({ id: match.id, persona: match.persona, dimension: match.dimension, stress_axis: match.stress_axis, requirement_id: match.requirement_id, procedure: match.procedure, evidence: String(grade.evidence).trim(), result: String(grade.result).trim() });
  }
  const notApplicable: any[] = [];
  for (const grade of graded.not_applicable || []) {
    const key = rriTScenarioIdentity(grade);
    const match = persistedByIdentity.get(key);
    if (!match) throw new Error(`not_applicable scenario ${key} is not in the persisted rri_t_scenarios artifact`);
    if (outcomes.has(key)) throw new Error(`scenario ${key} received more than one outcome`);
    outcomes.add(key);
    if (!String(grade.reason || "").trim()) throw new Error(`not_applicable scenario ${key} requires a concrete reason`);
    notApplicable.push({ id: match.id, persona: match.persona, dimension: match.dimension, stress_axis: match.stress_axis, requirement_id: match.requirement_id, reason: String(grade.reason).trim() });
  }
  return JSON.stringify({
    methodology: "rri-t",
    personas: persisted.content.personas || [],
    scenarios,
    not_applicable: [...(Array.isArray(persisted.content.not_applicable) ? persisted.content.not_applicable : []), ...notApplicable],
    open_blockers: persisted.content.open_blockers || [],
  });
}
