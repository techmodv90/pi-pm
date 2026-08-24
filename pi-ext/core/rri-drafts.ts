import { deleteRuntimeDraft, loadLatestRuntimeDraft, runtimeDraftPath, saveRuntimeDraft } from "./runtime-drafts.ts";

export interface RriDraftLineage {
  artifactId: string;
  contentHash: string;
}

interface RriDraftState {
  scanLineage: RriDraftLineage;
  state: unknown;
}

export function saveRriDraft(root: string, workItemId: string, scanLineage: RriDraftLineage, state: unknown): string {
  saveRuntimeDraft(root, "rri", workItemId, { scanLineage, state });
  return runtimeDraftPath(root, "rri", workItemId);
}

export function loadRriDraft(root: string, workItemId: string, scanLineage: RriDraftLineage): { workItemId: string; scanLineage: RriDraftLineage; state: unknown; updatedAt: string } {
  const draft = loadLatestRuntimeDraft<RriDraftState>(root, "rri", workItemId);
  if (draft.state.scanLineage?.artifactId !== scanLineage.artifactId || draft.state.scanLineage?.contentHash !== scanLineage.contentHash) throw new Error("RRI draft Scan lineage does not match the approved Scan checkpoint");
  return { workItemId, ...draft.state, updatedAt: draft.updatedAt };
}

export function deleteRriDraft(root: string, workItemId: string): void {
  deleteRuntimeDraft(root, "rri", workItemId);
}
