/**
 * /task-app command for the pi task-system extension.
 * Starts or opens the localhost web dashboard for centralized project management.
 */

import type { ExtensionAPI, ExtensionContext } from "@mariozechner/pi-coding-agent";
import { spawn, execSync } from "node:child_process";
import { existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import http from "node:http";

const DEFAULT_PORT = 4377;
const DEFAULT_HOST = "127.0.0.1";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

/**
 * Resolve the pic CLI binary path, using the same logic as cli-helpers.
 */
function findCliPath(): string {
  const configured = process.env.PIC_CLI || process.env.PI_TASK_SYSTEM_PIC;
  if (configured && existsSync(configured)) return configured;

  const extDir = resolve(__dirname, "..");
  const goBinaryName = process.platform === "win32" ? "pic.exe" : "pic";
  const userBin = resolve(process.env.HOME || "~", ".pi", "bin", goBinaryName);
  if (existsSync(userBin)) return userBin;

  const goBundled = resolve(extDir, "go-pic", "dist", goBinaryName);
  if (existsSync(goBundled)) return goBundled;

  const goKnown = resolve(process.env.HOME || "~", ".pi", "task-system", "go-pic", "dist", goBinaryName);
  if (existsSync(goKnown)) return goKnown;

  return "";
}

/**
 * Check if the web server is already running.
 * Returns true if the health endpoint responds.
 */
function getServerState(host: string, port: number): Promise<"go" | "other" | "down"> {
  return new Promise((resolve) => {
    const req = http.get(`http://${host}:${port}/healthz`, (res) => {
      let data = "";
      res.on("data", (chunk) => data += chunk);
      res.on("end", () => {
        try {
          const parsed = JSON.parse(data);
          if (parsed.ok === true && parsed.implementation === "go" && parsed.dashboard_assets === true) resolve("go");
          else resolve("other");
        } catch {
          resolve("other");
        }
      });
    });
    req.on("error", () => resolve("down"));
    req.setTimeout(1000, () => { req.destroy(); resolve("down"); });
  });
}

export function findDashboardDir(): string {
  const extDir = resolve(__dirname, "..");
  const candidates = [
    resolve(extDir, "go-pic", "web", "build"),
    resolve(process.env.HOME || "~", ".pi", "task-system", "go-pic", "web", "build"),
  ];
  return candidates.find((dir) => existsSync(resolve(dir, "index.html"))) || "";
}

function checkServerRunning(host: string, port: number): Promise<boolean> {
  return getServerState(host, port).then((state) => state === "go");
}

/**
 * Wait for the server to become healthy.
 * Polls /healthz up to maxAttempts times with a delay between each.
 */
function waitForServer(host: string, port: number, maxAttempts = 10, delayMs = 500): Promise<boolean> {
  return new Promise((resolve) => {
    let attempts = 0;
    const check = () => {
      attempts++;
      checkServerRunning(host, port).then((running) => {
        if (running) {
          resolve(true);
        } else if (attempts < maxAttempts) {
          setTimeout(check, delayMs);
        } else {
          resolve(false);
        }
      });
    };
    check();
  });
}

/**
 * Open a URL in the default browser (macOS).
 */
function openBrowser(url: string): void {
  try {
    execSync(`open "${url}"`, { stdio: "ignore", timeout: 5000 });
  } catch {
    // open not available or failed — silently ignore
  }
}

/**
 * Register the /task-app command.
 */
export function registerTaskAppCommand(pi: ExtensionAPI) {
  pi.registerCommand("task-app", {
    description: "Start or open the localhost web dashboard for project task management",
    handler: async (args: string, ctx: ExtensionContext) => {
      // Parse optional port/host from args
      const parts = args.trim().split(/\s+/).filter(Boolean);
      let port = DEFAULT_PORT;
      let host = DEFAULT_HOST;

      for (let i = 0; i < parts.length; i++) {
        if (parts[i] === "--port" && parts[i + 1]) {
          port = parseInt(parts[i + 1], 10);
          i++;
        } else if (parts[i] === "--host" && parts[i + 1]) {
          host = parts[i + 1];
          i++;
        }
      }

      // Check if the Go dashboard is already running. Older Node dashboards also answer /healthz.
      const serverState = await getServerState(host, port);
      if (serverState === "go") {
        const url = `http://${host}:${port}`;
        ctx.ui.notify(`Web dashboard is already running at ${url}`, "info");
        openBrowser(url);
        return;
      }
      if (serverState === "other") {
        ctx.ui.notify(`Port ${port} is already used by an older/non-Go dashboard. Stop that process, then run /task-app again.`, "error");
        return;
      }

      // Find the CLI binary
      const cliPath = findCliPath();
      if (!cliPath) {
        ctx.ui.notify(
          "Task system CLI not found. Make sure the task-system package is installed (try: pic init).",
          "error",
        );
        return;
      }

      const cliDir = dirname(cliPath);
      const dashboardDir = findDashboardDir();
      if (!dashboardDir) {
        ctx.ui.notify("Dashboard assets not found. Run the task-system build before /task-app.", "error");
        return;
      }

      // Spawn the web server in the background
      spawn(
        cliPath,
        ["web", "--port", String(port), "--host", host],
        {
          cwd: cliDir,
          env: { ...process.env, PIC_DASHBOARD_DIR: dashboardDir },
          stdio: "ignore",
          detached: true,
        },
      ).unref();

      const url = `http://${host}:${port}`;
      ctx.ui.notify(`Starting web dashboard at ${url}...`, "info");

      // Wait for the server to become healthy
      const started = await waitForServer(host, port);
      if (started) {
        ctx.ui.notify(`Web dashboard ready at ${url}`, "info");
        openBrowser(url);
      } else {
        ctx.ui.notify(
          `Web server may not have started. Check ${url} manually.`,
          "warning",
        );
      }
    },
  });
}
