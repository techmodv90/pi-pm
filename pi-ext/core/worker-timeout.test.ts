import assert from "node:assert/strict";
import test from "node:test";

import { MANAGED_BASH_TIMEOUT_MS, registerManagedBashTimeout } from "./worker-timeout.ts";

test("managed workers apply a bounded Bash timeout", () => {
  const previous = process.env.PI_TASK_PARENT_RUN_ID;
  process.env.PI_TASK_PARENT_RUN_ID = "parent-run";
  let handler: ((event: any) => void) | undefined;
  registerManagedBashTimeout({ on: (_name: string, callback: (event: any) => void) => { handler = callback; } } as any);

  const defaultCall = { toolName: "bash", input: {} };
  const shortCall = { toolName: "bash", input: { timeout: 1_000 } };
  const longCall = { toolName: "bash", input: { timeout: MANAGED_BASH_TIMEOUT_MS + 1 } };
  handler?.(defaultCall);
  handler?.(shortCall);
  handler?.(longCall);

  assert.equal(defaultCall.input.timeout, MANAGED_BASH_TIMEOUT_MS);
  assert.equal(shortCall.input.timeout, 1_000);
  assert.equal(longCall.input.timeout, MANAGED_BASH_TIMEOUT_MS);
  if (previous === undefined) delete process.env.PI_TASK_PARENT_RUN_ID;
  else process.env.PI_TASK_PARENT_RUN_ID = previous;
});