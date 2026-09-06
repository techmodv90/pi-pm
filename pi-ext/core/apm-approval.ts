/**
 * APM owner-approval gate: shows a generated .tasks.md in the Plannotator
 * browser UI (gated annotate session) and blocks until the owner approves,
 * approves with notes, annotates with feedback, or closes.
 *
 * Depends on @plannotator/pi-extension (declared pi dependency, see
 * package.json — the with-deps extension pattern). When the dependency or its
 * built UI assets are unavailable the tool returns { mode: "inline" } so the
 * agent asks the owner in conversation instead; it never approves on the
 * owner's behalf and never blocks on a missing UI.
 */

import { readFileSync } from "node:fs";
import { basename, resolve } from "node:path";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { Type } from "typebox";

/** Shape of the decision resolved by a Plannotator gated annotate session. */
export interface PlannotatorDecision {
  feedback?: string;
  annotations?: unknown[];
  approved?: boolean;
  exit?: boolean;
}

export interface ApprovalVerdict {
  outcome: "approved" | "approved_with_notes" | "feedback" | "closed";
  approved: boolean;
  feedback: string;
}

export function classifyApprovalDecision(decision: PlannotatorDecision): ApprovalVerdict {
  const feedback = (decision.feedback ?? "").trim();
  if (decision.approved === true) {
    return { outcome: feedback ? "approved_with_notes" : "approved", approved: true, feedback };
  }
  if (feedback) return { outcome: "feedback", approved: false, feedback };
  return { outcome: "closed", approved: false, feedback: "" };
}

export function registerApmApprovalTool(pi: ExtensionAPI): void {
  pi.registerTool({
    name: "apm_request_approval",
    label: "APM Request Approval",
    description:
      "Show an APM artifact (e.g. a generated .tasks.md) to the owner in the Plannotator browser UI and await their decision. " +
      "Returns mode \"plannotator\" with the verdict, or mode \"inline\" when Plannotator is unavailable — in that case ask the owner in conversation and wait for their reply instead.",
    promptSnippet: "Owner approval gate for APM artifacts; falls back to inline conversation approval.",
    parameters: Type.Object({
      filePath: Type.String({ description: "Path to the markdown artifact, relative to the working directory" }),
    }),

    async execute(_toolCallId, params, signal, _onUpdate, ctx) {
      const absolutePath = resolve(ctx.cwd, params.filePath);
      let markdown: string;
      try {
        markdown = readFileSync(absolutePath, "utf-8");
      } catch (err) {
        return {
          content: [{ type: "text" as const, text: `Error: cannot read ${absolutePath}: ${err instanceof Error ? err.message : String(err)}` }],
          details: {},
        };
      }

      // Detection is the dynamic import itself plus the built-UI check: no
      // Plannotator install or unbuilt assets → inline fallback for the agent.
      // Specifiers are built from a variable so `tsc --noEmit` does not
      // transitive-typecheck the dependency's TS sources (its code does not
      // pass our noUnusedLocals check); runtime resolution is unchanged.
      const ext = "@plannotator/pi-extension";
      let server: { url: string; waitForDecision: () => Promise<unknown>; stop: () => void };
      try {
        const annotateModule = (await import(`${ext}/server.ts`)) as {
          startAnnotateServer: (options: Record<string, unknown>) => Promise<{ url: string; waitForDecision: () => Promise<unknown>; stop: () => void }>;
        };
        const browserRuntime = (await import(`${ext}/plannotator-browser-runtime.ts`)) as {
          getPlanBrowserHtml: () => string;
          hasPlanBrowserHtml: () => boolean;
        };
        const networkModule = (await import(`${ext}/server/network.ts`)) as {
          openBrowser: (url: string) => Promise<void>;
        };
        if (!browserRuntime.hasPlanBrowserHtml()) {
          throw new Error("Plannotator UI assets are not built (plannotator.html missing)");
        }
        server = await annotateModule.startAnnotateServer({
          markdown,
          filePath: absolutePath,
          htmlContent: browserRuntime.getPlanBrowserHtml(),
          origin: "pi",
          mode: "annotate",
          gate: true,
          approvalNotesSupported: true,
          project: basename(ctx.cwd),
        });
        void networkModule.openBrowser(server.url).catch(() => {
          // Best-effort only; the URL is announced to the agent either way.
        });
      } catch (err) {
        return {
          content: [{
            type: "text" as const,
            text: JSON.stringify({
              mode: "inline",
              reason: err instanceof Error ? err.message : String(err),
              filePath: absolutePath,
              instruction: "Plannotator unavailable. Ask the owner in conversation to approve or annotate this file, and wait for their reply before advancing its status.",
            }, null, 2),
          }],
          details: { mode: "inline" },
        };
      }

      let decision: PlannotatorDecision;
      try {
        decision = (await new Promise<PlannotatorDecision>((res, rej) => {
          const onAbort = () => rej(new Error("approval session cancelled"));
          signal?.addEventListener("abort", onAbort, { once: true });
          server.waitForDecision().then(
            (value) => { signal?.removeEventListener("abort", onAbort); res(value as PlannotatorDecision); },
            (err) => { signal?.removeEventListener("abort", onAbort); rej(err); },
          );
        })) as PlannotatorDecision;
      } catch (err) {
        server.stop();
        return {
          content: [{
            type: "text" as const,
            text: JSON.stringify({
              mode: "inline",
              reason: err instanceof Error ? err.message : String(err),
              filePath: absolutePath,
              instruction: "Approval session did not complete. Ask the owner in conversation to approve or annotate this file.",
            }, null, 2),
          }],
          details: { mode: "inline" },
        };
      }
      server.stop();

      const verdict = classifyApprovalDecision(decision);
      return {
        content: [{
          type: "text" as const,
          text: JSON.stringify({ mode: "plannotator", filePath: absolutePath, ...verdict }, null, 2),
        }],
        details: { mode: "plannotator", ...verdict },
      };
    },
  });
}
