import type { ExtensionAPI, ExtensionContext } from "@mariozechner/pi-coding-agent";
import { matchesKey, Text } from "@mariozechner/pi-tui";
import { execPic, hasDb } from "../core/cli-helpers";


import { updateStatus } from "../core/extension-helpers";
import { buildTaskVerifyPrompt, buildWorkItemContinuePrompt } from "../tasking/work-item-prompts";
import type { PipelineScheduler } from "../pipeline/pipeline-scheduler.ts";

import { isTaskListRequest } from "../ui/task-navigation.ts";




/**
 * Start work on a Work Item through the persisted scheduler.
 */
async function startWorkOnWorkItem(ctx: ExtensionContext, pipelineScheduler: PipelineScheduler, workItemId: string): Promise<void> {
  const data = execPic(["show", workItemId], ctx.cwd);
  if (!data.work_item) {
    ctx.ui.notify(data.error || "Work Item not found.", "warning");
    return;
  }

  try {
    const result = await pipelineScheduler.start(workItemId, ctx);
    if (result.blocked) ctx.ui.notify(String(result.blocked), "warning");
    else if (result.contractorVerificationReady || result.ownerAcceptanceReady) ctx.ui.notify("Work Item pipeline reached its next persisted workflow gate.", "info");
    else ctx.ui.notify("Work Item pipeline started; Worker and Reviewer stages are persisted asynchronously.", "info");
  } catch (error) {
    ctx.ui.notify(error instanceof Error ? error.message : String(error), "error");
  }
}

export function registerTaskCommand(pi: ExtensionAPI, pipelineScheduler: PipelineScheduler) {
  pi.registerCommand("task", {
    description: "View and advance Work Items",
    handler: async (args, ctx) => {
      if (!ctx.hasUI) {
        ctx.ui.notify("/task requires interactive mode", "error");
        return;
      }

      const parts = args.trim().split(/\s+/).filter(Boolean);

      if (isTaskListRequest(parts)) {
        if (!hasDb(ctx.cwd)) {
          ctx.ui.notify("No task database found. Run /task init", "info");
          return;
        }
        await ctx.ui.custom<void>((_tui, _theme, _kb, done) => {
          const rows = execPic(["work-item", "list"], ctx.cwd);
          const text = Array.isArray(rows)
            ? rows.map((item: any) => `${item.status === "done" ? "[x]" : "[ ]"} ${item.type} ${item.title} (${item.id})`).join("\n")
            : rows.error || "No Work Items.";
          const browser = new Text(text || "No Work Items.", 1, 1);
          let focused = false;
          return {
            get focused() { return focused; },
            set focused(value: boolean) { focused = value; },
            render(width: number) { return browser.render(width); },
            invalidate() { browser.invalidate(); },
            handleInput(data: string) { if (matchesKey(data, "escape") || matchesKey(data, "ctrl+c")) done(); },
          };
        });
        return;
      }

      if (parts[0] === "search" && parts[1]) {
        const query = parts.slice(1).join(" ");
        const result = execPic(["search", query], ctx.cwd);
        ctx.ui.notify(result.error ? result.error : JSON.stringify(result, null, 2), result.error ? "error" : "info");
        return;
      }

      if (parts[0] === "init") {
        const result = execPic(["init"], ctx.cwd);
        ctx.ui.notify(result.error ? `Error: ${result.error}` : `Initialized: ${result.db_path || result.project?.name || "project"}`, result.error ? "error" : "info");
        updateStatus(ctx);
        return;
      }

      if (parts[0] === "help") {
        ctx.ui.notify("Task System:\n  /task                    List Work Items\n  /task <id>               View Work Item detail\n  /task work <id>          Advance an authorized Work Item\n  /task create <title> [-- description]  Create a Task Work Item\n  /task continue <id>      Continue an aggregate workflow\n  /task search <query>     Search Work Items\n  /task init               Initialize task database", "info");
        return;
      }

      if (parts[0] === "continue" && parts[1]) {
        const id = parts[1];
        const itemData = execPic(["show", id], ctx.cwd);
        const status = execPic(["work-item", "workflow-status", id], ctx.cwd);
        if (!itemData.work_item || status.error) {
          ctx.ui.notify(status.error || itemData.error || "Work Item not found", "error");
          return;
        }
        pi.sendUserMessage(status.next_stage === "contractor_verification"
          ? buildTaskVerifyPrompt(itemData)
          : buildWorkItemContinuePrompt(status, itemData.work_item), { deliverAs: "followUp" });
        return;
      }

      // Advance a Work Item through the persisted pipeline.
      if (parts[0] === "work" && parts[1]) {
        const workItemId = parts[1];
        const data = execPic(["show", workItemId], ctx.cwd);
        if (!data.work_item) { ctx.ui.notify(data.error || "Work Item not found", "error"); return; }
        await startWorkOnWorkItem(ctx, pipelineScheduler, workItemId);
        return;
      }

      // Create a new task; work advances it through the canonical workflow.
      if (parts[0] === "create") {
        const bodyParts = parts.slice(1);
        const descriptionSeparatorIndex = bodyParts.indexOf("--");
        const titleParts = descriptionSeparatorIndex >= 0 ? bodyParts.slice(0, descriptionSeparatorIndex) : bodyParts;
        const descriptionParts = descriptionSeparatorIndex >= 0 ? bodyParts.slice(descriptionSeparatorIndex + 1) : [];
        const title = titleParts.join(" ").trim();
        const description = descriptionParts.join(" ").trim();
        if (!title) { ctx.ui.notify("Usage: /task create <title> [-- brief description]", "error"); return; }
        const createArgs = ["work-item", "create", "task", title];
        if (description) createArgs.push("--description", description);
        const createResult = execPic(createArgs, ctx.cwd);
        if (createResult.error) { ctx.ui.notify(`Error creating task: ${createResult.error}`, "error"); return; }
        ctx.ui.notify(`Task created: ${title}${description ? " — description saved" : ""}`, "info");
        updateStatus(ctx);
        return;
      }

      // View a canonical Work Item by ID.
      const id = parts[0];
      await ctx.ui.custom<void>((_tui, theme, _kb, done) => {
        const data = execPic(["show", id], ctx.cwd);
        if (data.work_item) {
          const item = data.work_item;
          const lines = [
            theme.fg("accent", theme.bold(`# ${item.type}: ${item.title}`)),
            "",
            item.description || "",
            "",
            `Status: ${item.status}`,
            `Priority: ${item.priority}`,
            `Ready: ${data.ready === true ? "yes" : "no"}`,
          ];
          const txt = new Text(lines.join("\n"), 1, 1);
          let focused = false;
          return {
            get focused() { return focused; },
            set focused(value: boolean) { focused = value; },
            render(width: number) { return txt.render(width); },
            invalidate() { txt.invalidate(); },
            handleInput(data: string) {
              if (matchesKey(data, "escape") || matchesKey(data, "ctrl+c")) done();
            },
          };
        } else {
          const txt = new Text(theme.fg("dim", JSON.stringify(data, null, 2)), 1, 1);
          let focused = false;
          return {
            get focused() { return focused; },
            set focused(value: boolean) { focused = value; },
            render(width: number) { return txt.render(width); },
            invalidate() { txt.invalidate(); },
            handleInput(data: string) {
              if (matchesKey(data, "escape") || matchesKey(data, "ctrl+c")) done();
            },
          };
        }
      });
    },
  });
}
