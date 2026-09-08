package workitem

import (
	"reflect"
	"testing"
)

func TestNextActionHints(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		stage     string
		wantCount int
		wantID    string
		wantActor string
	}{
		{name: "implement", stage: "implement", wantCount: 1, wantID: "work_ready_leaves", wantActor: "contractor"},
		{name: "contractor_verification", stage: "contractor_verification", wantCount: 1, wantID: "verify_work_item", wantActor: "contractor"},
		{name: "aggregate_verification", stage: "aggregate_verification", wantCount: 1, wantID: "verify_aggregate", wantActor: "contractor"},
		{name: "owner_acceptance", stage: "owner_acceptance", wantCount: 1, wantID: "accept_aggregate", wantActor: "owner"},
		{name: "merge_pending", stage: "merge_pending", wantCount: 1, wantID: "merge_aggregate", wantActor: "contractor"},
		{name: "done yields no hints", stage: "done", wantCount: 0},
		{name: "unknown stage yields no hints", stage: "scan", wantCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hints := NextActionHints(tt.stage)
			if len(hints) != tt.wantCount {
				t.Fatalf("NextActionHints(%q) returned %d hints, want %d", tt.stage, len(hints), tt.wantCount)
			}
			if tt.wantCount == 0 {
				return
			}
			hint := hints[0]
			if hint.ID != tt.wantID || hint.Kind != "tool" || hint.Actor != tt.wantActor {
				t.Errorf("hint = %+v, want id %s kind tool actor %s", hint, tt.wantID, tt.wantActor)
			}
			if hint.Action == "" || hint.Label == "" {
				t.Errorf("hint %s must carry action and label, got %+v", tt.wantID, hint)
			}
		})
	}
}

func TestWithNextActions(t *testing.T) {
	t.Parallel()
	status := map[string]any{"next_stage": "implement"}
	got := WithNextActions(status)
	hints, ok := got["next_actions"].([]NextAction)
	if !ok || len(hints) != 1 {
		t.Fatalf("WithNextActions next_actions = %#v, want one NextAction", got["next_actions"])
	}
	if !reflect.DeepEqual(WithNextActions(map[string]any{"next_stage": "done"}), map[string]any{"next_stage": "done"}) {
		t.Error("WithNextActions must not attach hints for hint-less stages")
	}
	if !reflect.DeepEqual(WithNextActions(map[string]any{}), map[string]any{}) {
		t.Error("WithNextActions must not attach hints when next_stage is absent")
	}
}
