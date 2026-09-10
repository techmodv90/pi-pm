/** Shared helpers for /apm command handlers. */

import { createHash } from "node:crypto";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));

export function loadPrompt(name: string): string {
  return readFileSync(resolve(__dirname, "prompts", name), "utf8");
}

/** True when /apm init has created the workspace (.apm/prd). */
export function hasApmWorkspace(ctx: { cwd: string }): boolean {
  return existsSync(resolve(ctx.cwd, ".apm", "prd"));
}

/**
 * Send a loaded prompt as a hidden custom message into LLM context; steer
 * delivery avoids queueing behind the user's own followUp messages.
 */
export function sendHiddenPrompt(pi: ExtensionAPI, customType: string, prompt: string): void {
  pi.sendMessage(
    { customType, content: prompt, display: false },
    { triggerTurn: true },
  );
}

// --- Approval gates (hash-bound state, per gate-strengthening-proposal) ---

/**
 * Canonical body hash: sha1 of the file content with `## State:` lines
 * removed. The approval stamp binds this hash, so any edit outside the State
 * line invalidates the gate. Mirrors `sed '/^## State:/d' | shasum` — the
 * same recipe the /apm approve flow uses.
 */
export function gateBodyHash(content: string): string {
  const body = content
    .split("\n")
    .filter((line) => !/^##\s*State:/i.test(line))
    .join("\n");
  return createHash("sha1").update(body).digest("hex");
}

/**
 * Gate on a State-stamped artifact (proposals, designs, task lists). Fails
 * unless the file carries `## State: APPROVED hash=<gateBodyHash>` — missing
 * stamp reads as unapproved (fail-closed); hash mismatch means the artifact
 * changed since approval.
 */
export function assertApprovedState(path: string): void {
  const content = readFileSync(path, "utf8");
  const stamp = content.match(/^##\s*State:\s*(\S+)(?:\s+hash=(\S+))?\s*$/im);
  if (!stamp || stamp[1] !== "APPROVED" || !stamp[2]) {
    throw new Error(`${path} is not approved (no APPROVED stamp). Run /apm approve ${path} first.`);
  }
  if (stamp[2] !== gateBodyHash(content)) {
    throw new Error(`${path} changed since approval. Re-run /apm approve ${path}.`);
  }
}

/** Gate on a Gherkin spec: the @ready tag (set by /apm clarify) is required. */
export function assertReadySpec(path: string): void {
  const content = readFileSync(path, "utf8");
  if (!/^Feature:.*@ready/m.test(content)) {
    throw new Error(`${path} is not @ready. Run /apm clarify ${path} first.`);
  }
}

/** True when the artifact already carries an APPROVED state stamp. */
export function isApproved(path: string): boolean {
  try {
    assertApprovedState(path);
    return true;
  } catch {
    return false;
  }
}

/**
 * Run a gate check on `path` (when resolvable and existing); notify and fail
 * the command on gate violation. Null path = no gate applies, command proceeds.
 */
export function runGate(
  ctx: { ui: { notify(message: string, level?: "error" | "info" | "warning"): void } },
  path: string | null,
  check: (path: string) => void,
): boolean {
  if (!path || !existsSync(path)) return true;
  try {
    check(path);
    return true;
  } catch (e) {
    ctx.ui.notify((e as Error).message, "error");
    return false;
  }
}

/**
 * Resolve an artifact path from command input: a direct file path, or the
 * `Feature: <domain/Name>` / `Blueprint: <domain/Name>` / `Tasks: <domain/Name>`
 * form mapped under `.apm/specs/features/`. Null when neither form matches.
 */
export function resolveArtifactPath(cwd: string, arg: string, ext: string): string | null {
  const m = arg.match(/^(?:feature|blueprint|tasks|pseudocode):\s*(\S+)$/i);
  if (m) return resolve(cwd, ".apm", "specs", "features", `${m[1]}${ext}`);
  const trimmed = arg.trim();
  if (trimmed.endsWith(ext)) return resolve(cwd, trimmed);
  return null;
}
