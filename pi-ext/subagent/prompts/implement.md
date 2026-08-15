---
description: Full implementation workflow - scout gathers context, planner creates plan, worker implements
---
Use the task-system `subagent` tool with the chain parameter to execute this workflow:

1. First, use the "task-scout" agent to find all code relevant to: $@
2. Then, use the "task-planner" agent to create an implementation plan for "$@" using the context from the previous step (use {previous} placeholder)
3. Finally, use the "task-worker" pipeline through task_manager work_on_work_item to implement the approved plan.