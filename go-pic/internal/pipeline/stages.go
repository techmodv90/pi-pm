package pipeline

// Stages is the ordered lifecycle of pipeline_runs rows. Legacy planning
// stages remain listed so historical rows keep rendering; the scheduler no
// longer launches them.
var Stages = []string{"scan", "rri", "vision", "blueprint", "contracts", "task_graph", "worker", "review", "autofix"}
