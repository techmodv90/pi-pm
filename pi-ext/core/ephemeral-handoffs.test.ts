import assert from "node:assert/strict";
import test from "node:test";
import { EphemeralHandoffStore } from "./ephemeral-handoffs.ts";

test("ephemeral handoffs are bounded, expire, and remain readable until finalized", () => {
  let now = 100;
  const store = new EphemeralHandoffStore(10, 8, 10, () => now);
  const first = store.put("scan", "wi-1", "123456");
  assert.equal(store.get(first, "wi-1")?.payload, "123456");
  assert.equal(store.get(first, "wi-2"), undefined);

  const second = store.put("rri", "wi-2", "78901");
  assert.equal(store.get(first, "wi-1"), undefined);
  assert.equal(store.get(second, "wi-2")?.payload, "78901");
  assert.throws(() => store.put("scan", "wi-1", "123456789"), /exceeds 8 bytes/);

  store.deleteForWorkItem("wi-2", "scan");
  assert.equal(store.get(second, "wi-2")?.payload, "78901");
  store.deleteForWorkItem("wi-2", "rri");
  assert.equal(store.get(second, "wi-2"), undefined);

  const expiring = store.put("scan", "wi-3", "abc");
  now = 111;
  assert.equal(store.get(expiring, "wi-3"), undefined);
});