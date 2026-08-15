import assert from "node:assert/strict";
import test from "node:test";
import { WORKFLOW_PRIMER } from "./workflow-primer.ts";

test("workflow primer explains canonical tools, requirements, and stage order", () => {
  assert.match(WORKFLOW_PRIMER, /`show_work_item`.*`work_item_workflow_status`/s);
  assert.match(WORKFLOW_PRIMER, /scan.*rri.*vision.*blueprint.*contracts.*task_graph.*materialize.*authorize.*implement/s);
  assert.match(WORKFLOW_PRIMER, /requirements returned by `show_work_item` are authoritative/i);
  assert.match(WORKFLOW_PRIMER, /separate `Given`, `When`, and `Then` steps/);
  assert.match(WORKFLOW_PRIMER, /no `task_manager` action for direct requirement mutation/i);
  assert.match(WORKFLOW_PRIMER, /`materialize_work_item`.*`authorize_work_item_implementation`.*`work_on_work_item`/s);
  assert.match(WORKFLOW_PRIMER, /explicit owner approval/i);
});