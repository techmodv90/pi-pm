import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readFileSync, readdirSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { deleteRriDraft, loadRriDraft, saveRriDraft } from "./rri-drafts.ts";

test("RRI draft checkpoint atomically replaces one private lineage-bound file", () => {
  const root = mkdtempSync(join(tmpdir(), "rri-draft-"));
  const lineage = { artifactId: "wia-scan", contentHash: "sha256:scan" };

  const path = saveRriDraft(root, "wi-abc123", lineage, { answers: [1] });
  saveRriDraft(root, "wi-abc123", lineage, { answers: [1, 2] });

  assert.equal(statSync(path).mode & 0o777, 0o600);
  assert.deepEqual(loadRriDraft(root, "wi-abc123", lineage).state, { answers: [1, 2] });
  assert.equal(readFileSync(path, "utf8").includes('"answers":[1,2]'), true);
  assert.deepEqual(readdirSync(join(root, ".pi", "runtime", "rri")), ["wi-abc123.json"]);
});

test("RRI draft load rejects stale Scan lineage and delete removes the draft", () => {
  const root = mkdtempSync(join(tmpdir(), "rri-draft-"));
  const lineage = { artifactId: "wia-scan", contentHash: "sha256:scan" };
  const path = saveRriDraft(root, "wi-abc123", lineage, { answers: [] });

  assert.throws(() => loadRriDraft(root, "wi-abc123", { ...lineage, contentHash: "sha256:new" }), /Scan lineage/);
  deleteRriDraft(root, "wi-abc123");
  assert.equal(existsSync(path), false);
});