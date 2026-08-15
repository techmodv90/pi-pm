import { execFileSync } from "node:child_process";
import { isAbsolute } from "node:path";

export interface HerdrPanelHandle {
  paneId: string;
}

interface HerdrPanelOptions {
  env?: NodeJS.ProcessEnv;
  run?: (command: string, args: string[], env: NodeJS.ProcessEnv) => string;
}

interface OpenPanelInput {
  cwd: string;
  label: string;
  logPath: string;
}

export interface HerdrPanel {
  available(): boolean;
  open(input: OpenPanelInput): HerdrPanelHandle;
  close(handle: HerdrPanelHandle): void;
}

function paneIdFrom(stdout: string): string {
  const value = JSON.parse(stdout) as {
    pane_id?: unknown;
    pane?: { pane_id?: unknown };
    result?: { pane_id?: unknown; pane?: { pane_id?: unknown } };
  };
  const paneId = value.pane_id ?? value.pane?.pane_id ?? value.result?.pane_id ?? value.result?.pane?.pane_id;
  if (typeof paneId !== "string" || !paneId) throw new Error("HerdR did not return a pane ID");
  return paneId;
}

export function createHerdrPanel(options: HerdrPanelOptions = {}): HerdrPanel {
  const env = options.env ?? process.env;
  const run = options.run ?? ((command: string, args: string[], commandEnv: NodeJS.ProcessEnv) =>
    execFileSync(command, args, { encoding: "utf8", env: commandEnv, stdio: ["ignore", "pipe", "pipe"] }));
  const commandEnv = { ...env, HERDR_SOCKET_PATH: env.HERDR_SOCKET_PATH };
  const execute = (args: string[]) => run("herdr", args, commandEnv);

  return {
    available() {
      if (env.PI_TASK_HERDR_PANEL === "0") return false;
      if (env.HERDR_ENV !== "1" || !env.HERDR_PANE_ID || !env.HERDR_SOCKET_PATH || !isAbsolute(env.HERDR_SOCKET_PATH)) return false;
      try {
        const status = execute(["status", "server"]);
        if (/^compatible:\s*no\s*$/im.test(status)) return false;
        execute(["pane", "current", "--current"]);
        return true;
      } catch {
        return false;
      }
    },
    open(input) {
      const paneId = paneIdFrom(execute([
        "pane", "split", "--current",
        "--direction", "right",
        "--cwd", input.cwd,
        "--no-focus",
      ]));
      try {
        execute(["pane", "rename", paneId, input.label]);
        execute(["pane", "run", paneId, "tail", "-n", "+1", "-f", input.logPath]);
        return { paneId };
      } catch (error) {
        try { execute(["pane", "close", paneId]); } catch {}
        throw error;
      }
    },
    close(handle) {
      execute(["pane", "close", handle.paneId]);
    },
  };
}