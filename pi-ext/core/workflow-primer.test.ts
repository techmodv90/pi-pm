import assert from "node:assert/strict";
import test from "node:test";
import { WORKFLOW_PRIMER } from "./workflow-primer.ts";

test("workflow primer routes planning to the lean flow, not retired stages", () => {
  assert.match(WORKFLOW_PRIMER, /lean flow/i);
  assert.match(WORKFLOW_PRIMER, /\/apm start/);
  assert.match(WORKFLOW_PRIMER, /import-apm/);
  assert.match(WORKFLOW_PRIMER, /scan\/rri\/vision\/blueprint\/contracts\/task_graph\s+are retired/i);
  assert.match(WORKFLOW_PRIMER, /docs\/plans\/deletion-ledger\.json/);
});

test("workflow primer keeps execution authority rules", () => {
  assert.match(WORKFLOW_PRIMER, /`show_work_item`.*`work_item_workflow_status`/s);
  assert.match(WORKFLOW_PRIMER, /requirements returned by `show_work_item` are authoritative/i);
  assert.match(WORKFLOW_PRIMER, /TIPs are immutable/i);
  assert.match(WORKFLOW_PRIMER, /`authorize_work_item_implementation`.*`work_on_work_item`/s);
  assert.match(WORKFLOW_PRIMER, /immediately before its first worker claim/s);
  assert.match(WORKFLOW_PRIMER, /filtering runtime state, relabeling a failure, or bypassing a gate/);
  assert.match(WORKFLOW_PRIMER, /merge failure leaves `merge_pending`/);
});
