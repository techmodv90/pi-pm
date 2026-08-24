import { deleteRuntimeDraft, loadLatestRuntimeDraft, loadRuntimeDraft, saveRuntimeDraft } from "./runtime-drafts.ts";

export interface BlueprintDraft {
  workItemId: string;
  draftId: string;
  content: string;
  reviewed: boolean;
  checkpoint?: unknown;
  updatedAt: string;
}

interface BlueprintDraftState {
  content: string;
  reviewed: boolean;
  checkpoint?: unknown;
}

export function saveBlueprintDraft(root: string, workItemId: string, content: string, checkpoint?: unknown): BlueprintDraft {
  const draft = saveRuntimeDraft<BlueprintDraftState>(root, "blueprint", workItemId, {
    content,
    reviewed: checkpoint !== undefined,
    ...(checkpoint === undefined ? {} : { checkpoint }),
  });
  return { workItemId, draftId: draft.draftId, ...draft.state, updatedAt: draft.updatedAt };
}

export function loadBlueprintDraft(root: string, workItemId: string, draftId: string): BlueprintDraft {
  const draft = loadRuntimeDraft<BlueprintDraftState>(root, "blueprint", workItemId, draftId);
  return { workItemId, draftId: draft.draftId, ...draft.state, updatedAt: draft.updatedAt };
}

export function loadLatestBlueprintDraft(root: string, workItemId: string): BlueprintDraft {
  const draft = loadLatestRuntimeDraft<BlueprintDraftState>(root, "blueprint", workItemId);
  return { workItemId, draftId: draft.draftId, ...draft.state, updatedAt: draft.updatedAt };
}

export function deleteBlueprintDraft(root: string, workItemId: string): void {
  deleteRuntimeDraft(root, "blueprint", workItemId);
}
