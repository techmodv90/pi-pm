import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { buildAutoCommitMessage, findPicCli, withGitWriteLock } from "./cli-helpers.ts";

test("findPicCli prefers the installed Go binary when present", () => {
  const binary = process.platform === "win32" ? "pic.exe" : "pic";
  const userBin = resolve(process.env.HOME || "~", ".pi", "bin", binary);
  const goPic = resolve(dirname(fileURLToPath(import.meta.url)), "..", "go-pic", "dist", binary);
  const expected = existsSync(userBin) ? userBin : existsSync(goPic) ? goPic : null;
  if (expected) assert.equal(findPicCli(), expected);
});

test("realtime activity heartbeats use nonblocking pic execution", () => {
  const source = readFileSync(new URL("./activity-tracker.ts", import.meta.url), "utf8");
  assert.match(source, /execPicAsync/);
  assert.doesNotMatch(source, /\bexecPic\(/);
  assert.match(source, /now - lastHeartbeatAt < 5_000/);
});

test("buildAutoCommitMessage includes verification summary instead of only task title", () => {
  const message = buildAutoCommitMessage("Add chat group restriction table with audit trail", "t-s12j6pgp", {
    summary: "Index cleanup worker verified with targeted tests.",
    verificationStatus: "passed",
    changedFiles: ["internal/modules/chat-member/restriction_cleanup_worker.go", "internal/modules/chat-member/restriction_cleanup_worker_test.go"],
  });

  assert.match(message, /^verify\(t-s12j6pgp\): Index cleanup worker verified with targeted tests\./);
  assert.match(message, /Task ID: t-s12j6pgp/);
  assert.match(message, /Task: Add chat group restriction table with audit trail/);
  assert.match(message, /Verification status: passed/);
  assert.match(message, /Changed files:/);
  assert.match(message, /restriction_cleanup_worker_test\.go/);
});

test("buildAutoCommitMessage falls back to task title when no summary exists", () => {
  const message = buildAutoCommitMessage("Fix permission override", "t-abc123", { verificationStatus: "passed" });

  assert.match(message, /^verify\(t-abc123\): Fix permission override/);
  assert.match(message, /Verification status: passed/);
});

test("withGitWriteLock serializes repository writes and releases after completion", () => {
  const repo = mkdtempSync(join(tmpdir(), "task-system-git-lock-"));
  execFileSync("git", ["init", "-q"], { cwd: repo });
  let nestedError = "";
  withGitWriteLock(repo, () => {
    try { withGitWriteLock(repo, () => undefined); } catch (error) { nestedError = error instanceof Error ? error.message : String(error); }
  });
  assert.match(nestedError, /Git write transaction is active/);
  assert.doesNotThrow(() => withGitWriteLock(repo, () => undefined));
});
