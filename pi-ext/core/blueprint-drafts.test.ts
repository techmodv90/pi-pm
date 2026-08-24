import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, statSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { deleteBlueprintDraft, loadBlueprintDraft, loadLatestBlueprintDraft, saveBlueprintDraft } from "./blueprint-drafts.ts";

test("Blueprint drafts persist under the project root and can be replaced and deleted", () => {
  const root = mkdtempSync(join(tmpdir(), "pic-blueprint-"));
  try {
    const first = saveBlueprintDraft(root, "wi-abc123", '{"version":1}');
    const path = join(root, ".pi", "runtime", "blueprint", "wi-abc123.json");
    assert.equal(JSON.parse(readFileSync(path, "utf8")).state.content, '{"version":1}');
    assert.equal(statSync(path).mode & 0o777, 0o600);
    assert.equal(loadBlueprintDraft(root, "wi-abc123", first.draftId).reviewed, false);
    assert.equal(loadLatestBlueprintDraft(root, "wi-abc123").draftId, first.draftId);

    const reviewed = saveBlueprintDraft(root, "wi-abc123", '{"version":2}', { architecture: true });
    assert.notEqual(reviewed.draftId, first.draftId);
    assert.equal(loadBlueprintDraft(root, "wi-abc123", reviewed.draftId).reviewed, true);
    assert.throws(() => loadBlueprintDraft(root, "wi-abc123", first.draftId), /stale/);

    deleteBlueprintDraft(root, "wi-abc123");
    assert.throws(() => loadBlueprintDraft(root, "wi-abc123", reviewed.draftId));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
