---
description: Worker implements, reviewer reviews, worker applies feedback
---
Use task_manager work_on_work_item to start the persisted Worker -> Reviewer workflow for:

$@

The task-system scheduler owns Worker isolation, review dispatch, report persistence,
patch integration, and retry routing.