/**
 * Activity tracker for the live agent activity panel.
 * 
 * Maintains session-scoped in-memory state (currentWorkItemId, lastSkill) and writes
 * throttled heartbeat updates to session_activity via nonblocking child processes.
 * 
 * - currentWorkItemId: set from work_on_work_item tool results
 * - lastSkill: set from /skill: input events
 * - turn boundaries + agent_end: heartbeat writes
 */

import type { ExtensionAPI, ExtensionContext } from "@mariozechner/pi-coding-agent";
import { execPicAsync, hasDb } from "./cli-helpers";
import { resolve } from "node:path";

let currentWorkItemId = "";
let lastSkill = "";
let sessionId = "";
let lastHeartbeatAt = 0;
let heartbeatInFlight = false;

/**
 * Generate a stable per-session id.
 * Uses the session file basename when available, falling back to a short
 * random suffix so concurrent sessions on the same machine don't collide.
 */
function generateSessionId(ctx: ExtensionContext): string {
  const sessionFile = ctx.sessionManager.getSessionFile?.();
  if (sessionFile) {
    // ponytail: session file paths are unique per session
    return `pi-${resolve(sessionFile).split("/").pop()?.replace(/\.jsonl$/, "") || "session"}`;
  }
  return `pi-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
}

function heartbeat(ctx: ExtensionContext, status: string = "active", force = false) {
  if (!hasDb(ctx.cwd) || heartbeatInFlight) return;
  const now = Date.now();
  if (!force && now - lastHeartbeatAt < 5_000) return;
  lastHeartbeatAt = now;
  heartbeatInFlight = true;
  void execPicAsync([
    "activity", "update",
    "--session", sessionId,
    "--task", currentWorkItemId,
    "--status", status,
    "--skill", lastSkill,
  ], ctx.cwd).finally(() => { heartbeatInFlight = false; });
}

export function registerActivityTracker(pi: ExtensionAPI) {
  // ── Session lifecycle ──────────────────────────────────────
  pi.on("session_start", async (_event, ctx) => {
    sessionId = generateSessionId(ctx);
    currentWorkItemId = "";
    lastSkill = "";
  });

  // Capture the Work Item selected by the canonical pipeline action.
  pi.on("tool_result", async (event, ctx) => {
    if (event.toolName !== "task_manager") return;
    const details = event.details as any;
    if (details?.action === "work_on_work_item" && details?.workItem?.id) {
      currentWorkItemId = String(details.workItem.id);
      lastSkill = ""; // reset skill on new work session
      heartbeat(ctx, "active", true);
    }
  });

  // ── Skill: capture from /skill: input ──────────────────────
  pi.on("input", async (event, ctx) => {
    const m = event.text.match(/^\/skill:(\S+)/);
    if (m) {
      lastSkill = m[1];
      heartbeat(ctx);
    }
    // ponytail: don't block input, just observe
  });

  // ── Heartbeat on turn boundaries ───────────────────────────
  pi.on("turn_start", async (_event, ctx) => {
    heartbeat(ctx);
  });

  pi.on("turn_end", async (_event, ctx) => {
    heartbeat(ctx);
  });

  // ── Idle on agent end ──────────────────────────────────────
  pi.on("agent_end", async (_event, ctx) => {
    heartbeat(ctx, "idle", true);
  });
}
