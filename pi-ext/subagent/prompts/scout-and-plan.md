---
description: Scout gathers context, planner creates a plan without implementation
---
Use the task-system `subagent` tool with the chain parameter:

1. First, use the "task-scout" agent to find all code relevant to: $@
2. Then, use the "task-planner" agent to create an implementation plan for "$@" using the context from the previous step (use {previous} placeholder)

Do not implement. Return the plan to the contractor.