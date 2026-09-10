/** Tests for hash-bound approval gates (apm-shared.ts). */

import assert from "node:assert/strict";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import {
  assertApprovedState,
  assertReadySpec,
  gateBodyHash,
  resolveArtifactPath,
} from "./apm-shared.ts";

test("gateBodyHash ignores the State line only", () => {
  const body = "# Title\n\nSome content\n";
  const pending = `${body}## State: PENDING\n`;
  const approved = `${body}## State: APPROVED hash=abc\n`;
  assert.equal(gateBodyHash(pending), gateBodyHash(approved));
  assert.notEqual(gateBodyHash(pending), gateBodyHash(`${body}edited\n## State: PENDING\n`));
});

test("assertApprovedState fail-closed without stamp", () => {
  const dir = mkdtempSync(join(tmpdir(), "apm-gate-"));
  try {
    const file = join(dir, "x-proposal.md");
    writeFileSync(file, "# T\ncontent\n");
    assert.throws(() => assertApprovedState(file), /not approved/);

    writeFileSync(file, "# T\ncontent\n## State: PENDING\n");
    assert.throws(() => assertApprovedState(file), /not approved/);

    // Stamp without hash → unapproved (fail-closed).
    writeFileSync(file, "# T\ncontent\n## State: APPROVED\n");
    assert.throws(() => assertApprovedState(file), /not approved/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("assertApprovedState accepts matching hash, rejects drift", () => {
  const dir = mkdtempSync(join(tmpdir(), "apm-gate-"));
  try {
    const file = join(dir, "x-proposal.md");
    writeFileSync(file, "# T\ncontent\n");
    const hash = gateBodyHash("# T\ncontent\n");
    writeFileSync(file, `# T\ncontent\n## State: APPROVED hash=${hash}\n`);
    assertApprovedState(file); // no throw

    writeFileSync(file, `# T\ncontent CHANGED\n## State: APPROVED hash=${hash}\n`);
    assert.throws(() => assertApprovedState(file), /changed since approval/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("assertReadySpec requires @ready on the Feature line", () => {
  const dir = mkdtempSync(join(tmpdir(), "apm-gate-"));
  try {
    const file = join(dir, "x.feature");
    writeFileSync(file, "Feature: payments/Charge @draft\n  Scenario: ok\n");
    assert.throws(() => assertReadySpec(file), /not @ready/);

    writeFileSync(file, "Feature: payments/Charge @ready\n  Scenario: ok\n");
    assertReadySpec(file); // no throw
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("resolveArtifactPath maps direct paths and label forms", () => {
  const cwd = "/repo";
  assert.equal(
    resolveArtifactPath(cwd, "docs/x.tasks.md", ".tasks.md"),
    "/repo/docs/x.tasks.md",
  );
  assert.equal(
    resolveArtifactPath(cwd, "Tasks: payments/Charge", ".tasks.md"),
    "/repo/.apm/specs/features/payments/Charge.tasks.md",
  );
  assert.equal(
    resolveArtifactPath(cwd, "Blueprint: payments/Charge", ".plan.md"),
    "/repo/.apm/specs/features/payments/Charge.plan.md",
  );
  assert.equal(resolveArtifactPath(cwd, "nothing matching here", ".tasks.md"), null);
});
