package stage

// Stages is the ordered lifecycle of pipeline_runs rows. Legacy planning
// stages remain listed so historical rows keep rendering; the scheduler no
// longer launches them.
var Stages = []string{"scan", "rri", "vision", "blueprint", "contracts", "task_graph", "worker", "review", "autofix"}

// Contains reports whether the ordered lifecycle includes name.
func Contains(name string) bool {
	for _, candidate := range Stages {
		if candidate == name {
			return true
		}
	}
	return false
}
