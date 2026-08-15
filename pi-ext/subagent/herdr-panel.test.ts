import assert from "node:assert/strict";
import test from "node:test";

import { createHerdrPanel } from "./herdr-panel.ts";

test("HerdR panel auto-detection requires a healthy managed pane", () => {
  const calls: string[][] = [];
  const panel = createHerdrPanel({
    env: {
      HERDR_ENV: "1",
      HERDR_PANE_ID: "w1:p1",
      HERDR_SOCKET_PATH: "/tmp/herdr.sock",
    },
    run: (_command, args) => {
      calls.push(args);
      return args[0] === "status" ? "status: running\ncompatible: yes" : '{"result":{"pane":{"pane_id":"w1:p1"}}}';
    },
  });

  assert.equal(panel.available(), true);
  assert.deepEqual(calls, [["status", "server"], ["pane", "current", "--current"]]);
});

test("HerdR panel launches unfocused beside the parent and closes its pane", () => {
  const calls: string[][] = [];
  const panel = createHerdrPanel({
    env: {
      HERDR_ENV: "1",
      HERDR_PANE_ID: "w1:p1",
      HERDR_SOCKET_PATH: "/tmp/herdr.sock",
    },
    run: (_command, args) => {
      calls.push(args);
      if (args[0] === "pane" && args[1] === "split") {
        return '{"result":{"pane":{"pane_id":"w1:p2","terminal_id":"term-2"}}}';
      }
      return "";
    },
  });

  const handle = panel.open({
    cwd: "/tmp/worktree",
    label: "task-worker-t-42",
    logPath: "/tmp/agent output.log",
  });
  panel.close(handle);

  assert.deepEqual(calls, [
    ["pane", "split", "--current", "--direction", "right", "--cwd", "/tmp/worktree", "--no-focus"],
    ["pane", "rename", "w1:p2", "task-worker-t-42"],
    ["pane", "run", "w1:p2", "tail", "-n", "+1", "-f", "/tmp/agent output.log"],
    ["pane", "close", "w1:p2"],
  ]);
});

test("HerdR panel rejects an incompatible running server", () => {
  const panel = createHerdrPanel({
    env: { HERDR_ENV: "1", HERDR_PANE_ID: "w1:p1", HERDR_SOCKET_PATH: "/tmp/herdr.sock" },
    run: (_command, args) => args[0] === "status"
      ? "status: running\ncompatible: no"
      : '{"result":{"pane":{"pane_id":"w1:p1"}}}',
  });

  assert.equal(panel.available(), false);
});