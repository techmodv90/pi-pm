import test from "node:test";
import assert from "node:assert/strict";
import { buildVerificationItemsJsonWithCommit } from "./verification-commit.ts";

test("buildVerificationItemsJsonWithCommit injects commit hash into linked verification items", () => {
  const result = buildVerificationItemsJsonWithCommit(JSON.stringify([{ requirement_id: "req-1", status: "pass", evidence: "Tests pass" }]), "abc1234");

  assert.equal(result.error, undefined);
  assert.deepEqual(JSON.parse(result.itemsJson || "[]"), [{ requirement_id: "req-1", status: "pass", evidence: "Tests pass", commit: "abc1234" }]);
});

test("buildVerificationItemsJsonWithCommit preserves explicit item commit hashes", () => {
  const result = buildVerificationItemsJsonWithCommit(JSON.stringify([{ requirement_id: "req-1", status: "pass", commit_hash: "existing" }]), "abc1234");

  assert.equal(result.error, undefined);
  assert.deepEqual(JSON.parse(result.itemsJson || "[]"), [{ requirement_id: "req-1", status: "pass", commit_hash: "existing", commit: "existing" }]);
});

test("buildVerificationItemsJsonWithCommit rejects missing verification items", () => {
  const result = buildVerificationItemsJsonWithCommit(undefined, "abc1234");

  assert.match(result.error || "", /requires items_json/);
  assert.equal(result.itemsJson, undefined);
});

test("buildVerificationItemsJsonWithCommit rejects unlinked verification items", () => {
  const result = buildVerificationItemsJsonWithCommit(JSON.stringify([{ status: "pass", evidence: "Tests pass" }]), "abc1234");

  assert.match(result.error || "", /requires requirement_id/);
  assert.equal(result.itemsJson, undefined);
});

test("buildVerificationItemsJsonWithCommit rejects non-array JSON", () => {
  const result = buildVerificationItemsJsonWithCommit(JSON.stringify({ status: "pass" }), "abc1234");

  assert.match(result.error || "", /must be an array/);
  assert.equal(result.itemsJson, undefined);
});
