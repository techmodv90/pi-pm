import { existsSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

export function buildPiInvocation(args: string[], script = process.argv[1]): { command: string; args: string[] } {
  const virtualScript = script?.startsWith("/$bunfs/root/");
  if (script && !virtualScript && existsSync(script)) return { command: process.execPath, args: [script, ...args] };
  const runtime = process.execPath.toLowerCase().split(/[\\/]/).pop() || "";
  if (!/^(node|bun)(\.exe)?$/.test(runtime)) return { command: process.execPath, args };
  return { command: "pi", args };
}

export function getAppendSystemPromptPaths(agentName: string, systemPrompt: string, promptPath: string, globalPromptPath = join(homedir(), ".pi", "agent", "APPEND_SYSTEM.md")): string[] {
  return [agentName === "task-worker" && existsSync(globalPromptPath) ? globalPromptPath : "", systemPrompt.trim() ? promptPath : ""].filter(Boolean);
}

export function parseJsonEvent(line: string): any | null {
  if (!line.trim()) return null;
  try {
    return JSON.parse(line);
  } catch {
    return null;
  }
}

export function finalAssistantText(messages: any[]): string {
  for (let index = messages.length - 1; index >= 0; index--) {
    const message = messages[index];
    if (message?.role !== "assistant") continue;
    const text = message.content?.find?.((part: any) => part.type === "text")?.text;
    if (text) return text;
  }
  return "";
}
