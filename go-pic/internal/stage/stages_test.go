package stage

import "testing"

func TestStages(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"scan", "rri", "vision", "blueprint", "contracts", "task_graph", "worker", "review", "autofix"} {
		if !Contains(name) {
			t.Errorf("Contains(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "deploy", "verify", "SCAN"} {
		if Contains(name) {
			t.Errorf("Contains(%q) = true, want false", name)
		}
	}
}
