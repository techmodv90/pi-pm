export interface ParsedVerificationItems {
  items: Record<string, unknown>[];
  error?: string;
}

/**
 * Parse verification items for commit traceability injection.
 * Expects a JSON array string of requirement-linked items and returns parsed
 * items, rejecting missing traceability before any verification commit.
 */
export function parseVerificationItemsForCommit(itemsJson?: string): ParsedVerificationItems {
  if (!itemsJson || !itemsJson.trim()) return { items: [], error: "Error: passed verification requires items_json with requirement_id before commit traceability can be attached" };
  try {
    const parsed = JSON.parse(itemsJson);
    if (!Array.isArray(parsed)) return { items: [], error: "Error: verification items JSON must be an array before commit traceability can be attached" };
    if (parsed.length === 0) return { items: [], error: "Error: passed verification requires at least one requirement-linked verification item" };
    const missingTraceIndex = parsed.findIndex((item) => !item?.requirement_id);
    if (missingTraceIndex >= 0) return { items: [], error: `Error: verification item ${missingTraceIndex + 1} requires requirement_id before commit traceability can be attached` };
    return { items: parsed };
  } catch {
    return { items: [], error: "Error: verification items JSON must be valid JSON before commit traceability can be attached" };
  }
}

/**
 * Attach a commit hash to verification items.
 * Expects parsed verification item objects and a commit hash; returns a new
 * array that preserves explicit item commit hashes and fills missing ones.
 */
export function attachCommitToVerificationItems(items: Record<string, unknown>[], commitHash: string): Record<string, unknown>[] {
  return items.map((item) => ({ ...item, commit: item.commit || item.commit_hash || commitHash }));
}

/**
 * Build verification items JSON with commit traceability applied.
 * Expects optional items JSON and a commit hash; returns JSON text or a parse
 * error suitable for task_manager tool responses.
 */
export function buildVerificationItemsJsonWithCommit(itemsJson: string | undefined, commitHash: string): { itemsJson?: string; error?: string } {
  const parsedItems = parseVerificationItemsForCommit(itemsJson);
  if (parsedItems.error) return { error: parsedItems.error };
  return { itemsJson: JSON.stringify(attachCommitToVerificationItems(parsedItems.items, commitHash)) };
}
