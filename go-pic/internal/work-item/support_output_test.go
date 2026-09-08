package workitem

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateGherkinSteps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		wantErr string
	}{
		{
			name: "all three steps",
			text: "Given a task\nWhen the worker runs\nThen the task closes",
		},
		{
			name: "case insensitive with bullets",
			text: "- given a task\n* when it runs\n- then it closes",
		},
		{
			name:    "missing then",
			text:    "Given a task\nWhen the worker runs",
			wantErr: "require Given, When, and Then steps",
		},
		{
			name:    "empty text",
			text:    "",
			wantErr: "require Given, When, and Then steps",
		},
		{
			name:    "prose without steps",
			text:    "just a sentence about work",
			wantErr: "require Given, When, and Then steps",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGherkinSteps(tt.text)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateGherkinSteps(%q) unexpected error: %v", tt.text, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateGherkinSteps(%q) error = %v, want contains %q", tt.text, err, tt.wantErr)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	t.Parallel()
	if got := firstNonEmpty("", "second", "third"); got != "second" {
		t.Errorf("firstNonEmpty = %q, want %q", got, "second")
	}
	if got := firstNonEmpty(); got != "" {
		t.Errorf("firstNonEmpty() = %q, want empty", got)
	}
}

func TestValidPlanningDepth(t *testing.T) {
	t.Parallel()
	for _, depth := range ValidPlanningDepths {
		if !ValidPlanningDepth(depth) {
			t.Errorf("ValidPlanningDepth(%q) = false, want true", depth)
		}
	}
	if ValidPlanningDepth("designed-full") {
		t.Error(`ValidPlanningDepth("designed-full") = true, want false`)
	}
}

func TestAddEventWithModel(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	if err := addEventWithModel(db, "wi-1", "execution_reset", "owner", "model-1", "summary text", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("addEventWithModel: %v", err)
	}
	row, err := queryOne(db, `SELECT event_type, actor_role, actor_model, payload_json FROM work_item_events WHERE work_item_id=?`, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if row["event_type"] != "execution_reset" || row["actor_role"] != "owner" || row["actor_model"] != "model-1" {
		t.Errorf("event row = %v, want persisted event fields", row)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(row["payload_json"].(string)), &payload); err != nil {
		t.Fatalf("payload_json not valid JSON: %v", err)
	}
	if payload["k"] != "v" {
		t.Errorf("payload = %v, want k=v", payload)
	}
}

func TestAddEventDefaultsEmptyModel(t *testing.T) {
	t.Parallel()
	db := testDB(t)
	if err := addEvent(db, "wi-1", "completed", "contractor", "done", nil); err != nil {
		t.Fatalf("addEvent: %v", err)
	}
	row, err := queryOne(db, `SELECT actor_model, payload_json FROM work_item_events WHERE work_item_id=?`, "wi-1")
	if err != nil {
		t.Fatal(err)
	}
	if row["actor_model"] != "" {
		t.Errorf("actor_model = %v, want empty for addEvent", row["actor_model"])
	}
	if row["payload_json"] != "null" {
		t.Errorf("payload_json = %v, want null for nil payload", row["payload_json"])
	}
}

func TestOutputOne(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`INSERT INTO work_item_labels VALUES('wi-1','a')`); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := outputOne(db, `SELECT label FROM work_item_labels WHERE work_item_id=?`, "wi-1"); err != nil {
			t.Errorf("outputOne: %v", err)
		}
	})
	if !strings.Contains(out, `"label":"a"`) {
		t.Errorf("outputOne printed %q, want the row JSON", out)
	}
	if err := outputOne(db, `SELECT label FROM work_item_labels WHERE work_item_id='missing'`); err == nil {
		t.Error("outputOne miss: want error, got nil")
	}
}
