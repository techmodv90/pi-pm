import { randomUUID } from "node:crypto";
import { chmodSync, mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

export interface RriDraftLineage {
  artifactId: string;
  contentHash: string;
}

interface RriDraft {
  workItemId: string;
  scanLineage: RriDraftLineage;
  state: unknown;
  updatedAt: string;
}

function draftPath(root: string, workItemId: string): string {
  if (!/^wi-[a-z0-9]+$/.test(workItemId)) throw new Error("Invalid Work Item ID for RRI draft");
  return join(root, ".pi", "runtime", "rri", `${workItemId}.json`);
}

export function saveRriDraft(root: string, workItemId: string, scanLineage: RriDraftLineage, state: unknown): string {
  const path = draftPath(root, workItemId);
  const directory = dirname(path);
  const temporary = `${path}.${process.pid}-${randomUUID()}.tmp`;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  chmodSync(directory, 0o700);
  const draft: RriDraft = { workItemId, scanLineage, state, updatedAt: new Date().toISOString() };
  writeFileSync(temporary, JSON.stringify(draft), { encoding: "utf8", mode: 0o600 });
  renameSync(temporary, path);
  chmodSync(path, 0o600);
  return path;
}

export function loadRriDraft(root: string, workItemId: string, scanLineage: RriDraftLineage): RriDraft {
  const draft = JSON.parse(readFileSync(draftPath(root, workItemId), "utf8")) as RriDraft;
  if (draft.workItemId !== workItemId || draft.scanLineage?.artifactId !== scanLineage.artifactId || draft.scanLineage?.contentHash !== scanLineage.contentHash) {
    throw new Error("RRI draft Scan lineage does not match the approved Scan checkpoint");
  }
  return draft;
}

export function deleteRriDraft(root: string, workItemId: string): void {
  rmSync(draftPath(root, workItemId), { force: true });
}