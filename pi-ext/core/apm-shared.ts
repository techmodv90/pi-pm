/** Shared helpers for /apm command handlers. */

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
