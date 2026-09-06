import { test } from "node:test";
import assert from "node:assert/strict";
import { classifyApprovalDecision } from "./apm-approval.ts";

test("approve with no notes", () => {
  const v = classifyApprovalDecision({ feedback: "", annotations: [], approved: true });
  assert.deepEqual(v, { outcome: "approved", approved: true, feedback: "" });
});

test("approve with notes stays approved and carries the notes", () => {
  const v = classifyApprovalDecision({ feedback: "rename T004", approved: true });
  assert.deepEqual(v, { outcome: "approved_with_notes", approved: true, feedback: "rename T004" });
});

test("feedback without approval is a change request", () => {
  const v = classifyApprovalDecision({ feedback: "T012 conflicts with T011" });
  assert.deepEqual(v, { outcome: "feedback", approved: false, feedback: "T012 conflicts with T011" });
});

test("closed session is not an approval", () => {
  const v = classifyApprovalDecision({ feedback: "", annotations: [] });
  assert.deepEqual(v, { outcome: "closed", approved: false, feedback: "" });
});
