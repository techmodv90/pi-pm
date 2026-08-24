import { randomUUID } from "node:crypto";
import { chmodSync, mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

export interface RuntimeDraft<T = unknown> {
  stage: string;
  workItemId: string;
  draftId: string;
  state: T;
  updatedAt: string;
}

export function runtimeDraftPath(root: string, stage: string, workItemId: string): string {
  if (!/^[a-z][a-z0-9_-]*$/.test(stage) || !/^wi-[a-z0-9]+$/.test(workItemId)) throw new Error("Invalid runtime draft identity");
  return join(root, ".pi", "runtime", stage, `${workItemId}.json`);
}

function draftPath(root: string, stage: string, workItemId: string): string {
  return runtimeDraftPath(root, stage, workItemId);
}

export function saveRuntimeDraft<T>(root: string, stage: string, workItemId: string, state: T): RuntimeDraft<T> {
  const path = draftPath(root, stage, workItemId);
  const directory = dirname(path);
  const draft: RuntimeDraft<T> = { stage, workItemId, draftId: `draft-${randomUUID()}`, state, updatedAt: new Date().toISOString() };
  const temporary = `${path}.${process.pid}-${randomUUID()}.tmp`;
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  chmodSync(directory, 0o700);
  writeFileSync(temporary, JSON.stringify(draft), { encoding: "utf8", mode: 0o600 });
  renameSync(temporary, path);
  chmodSync(path, 0o600);
  return draft;
}

export function loadLatestRuntimeDraft<T>(root: string, stage: string, workItemId: string): RuntimeDraft<T> {
  return JSON.parse(readFileSync(draftPath(root, stage, workItemId), "utf8")) as RuntimeDraft<T>;
}

export function loadRuntimeDraft<T>(root: string, stage: string, workItemId: string, draftId: string): RuntimeDraft<T> {
  const draft = loadLatestRuntimeDraft<T>(root, stage, workItemId);
  if (draft.stage !== stage || draft.workItemId !== workItemId || draft.draftId !== draftId) throw new Error("Runtime draft is stale or does not belong to this Work Item");
  return draft;
}

export function deleteRuntimeDraft(root: string, stage: string, workItemId: string): void {
  rmSync(draftPath(root, stage, workItemId), { force: true });
}
