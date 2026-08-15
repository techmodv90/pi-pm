# Task-System Subagents

This directory is the task-system-owned adaptation of the upstream subagent extension.

It keeps the upstream execution model:

- discover Markdown agent definitions;
- start isolated `pi --mode json -p --no-session` child processes;
- parse JSONL message and tool-result events incrementally;
- stream updates through the task-system `subagent` tool;
- support single, parallel, and chained orchestration.

The durable task pipeline remains the owner of claims, leases, Worker worktrees,
completion reports, review gates, and resume/reconciliation. Worker and Reviewer
pipeline stages use `runner.ts` directly; they do not call another extension.