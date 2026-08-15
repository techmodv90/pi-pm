import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { buildTaskReportPath, openTaskReportFile, writeTaskReportFile } from "./task-report-file.ts";

test("buildTaskReportPath uses task-system temp directory and html extension", () => {
  const tempRoot = mkdtempSync(join(tmpdir(), "pi-report-path-"));
  try {
    const filePath = buildTaskReportPath("t-unsafe:/name", new Date("2026-05-10T08:00:00.000Z"), tempRoot);
    assert.match(filePath, /pi-task-system\/reports/);
    assert.match(filePath, /t-unsafe-name-2026-05-10T08-00-00-000Z\.html$/);
  } finally {
    rmSync(tempRoot, { recursive: true, force: true });
  }
});

test("writeTaskReportFile creates directories and writes html content", () => {
  const tempRoot = mkdtempSync(join(tmpdir(), "pi-report-write-"));
  try {
    const html = "<html><body>hello</body></html>";
    const filePath = writeTaskReportFile("t-123", html, new Date("2026-05-10T08:00:00.000Z"), tempRoot);
    const saved = readFileSync(filePath, "utf-8");
    assert.equal(saved, html);
    assert.match(dirname(filePath), /pi-task-system\/reports/);
  } finally {
    rmSync(tempRoot, { recursive: true, force: true });
  }
});

test("openTaskReportFile returns structured error when opener fails", () => {
  const result = openTaskReportFile("/tmp/report.html", () => ({ ok: false, error: "open failed" }));
  assert.deepEqual(result, { ok: false, error: "open failed" });
});
