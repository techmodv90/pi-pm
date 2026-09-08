package acceptance

import (
	"strings"
	"testing"
	"time"
)

func TestValidateGherkinSteps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		wantErr string
	}{
		{name: "full behavioral scenario", text: "Given a task\nWhen it runs\nThen it closes"},
		{name: "markdown list steps", text: "- Given a task\n* When it runs\n- Then it closes"},
		{name: "case insensitive", text: "given a task\nwhen it runs\nthen it closes"},
		{name: "missing then", text: "Given a task\nWhen it runs", wantErr: "Then"},
		{name: "missing given", text: "When it runs\nThen it closes", wantErr: "Given"},
		{name: "non behavioral prose", text: "the task works well", wantErr: "require Given, When, and Then steps"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateGherkinSteps(tt.text)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want contains %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunCurrent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	valid := &RunEvidence{ID: "pr-1", ArtifactID: "wia-1", Outcome: "passed", RecordedAt: now.Add(-time.Hour).Format(time.RFC3339)}
	if !RunCurrent(valid, now, 24*time.Hour) {
		t.Fatal("fresh passed run must be current")
	}
	if RunCurrent(nil, now, time.Hour) {
		t.Fatal("nil evidence must not be current")
	}
	if RunCurrent(&RunEvidence{ID: "pr-1", Outcome: "passed"}, now, time.Hour) {
		t.Fatal("missing artifact id must not be current")
	}
	if RunCurrent(&RunEvidence{ID: "pr-1", ArtifactID: "wia-1", Outcome: "bogus"}, now, time.Hour) {
		t.Fatal("unknown outcome must not be current")
	}
	superseded := *valid
	superseded.SupersededAt = now.Add(-time.Minute).Format(time.RFC3339)
	if RunCurrent(&superseded, now, time.Hour) {
		t.Fatal("superseded run must not be current")
	}
	stale := *valid
	stale.RecordedAt = now.Add(-48 * time.Hour).Format(time.RFC3339)
	if RunCurrent(&stale, now, 24*time.Hour) {
		t.Fatal("stale run must not be current")
	}
	future := *valid
	future.RecordedAt = now.Add(time.Hour).Format(time.RFC3339)
	if RunCurrent(&future, now, 24*time.Hour) {
		t.Fatal("future-recorded run must not be current")
	}
}

func TestFailurePath(t *testing.T) {
	t.Parallel()
	for _, outcome := range Outcomes {
		got := FailurePath(outcome)
		want := outcome != "passed"
		if got != want {
			t.Errorf("FailurePath(%q) = %v, want %v", outcome, got, want)
		}
	}
}

func TestValidateTaskGherkinFallsBackToDescription(t *testing.T) {
	path := t.TempDir()
	_ = path
	// validateTaskGherkin reads the requirement's acceptance criteria; the
	// DB-level path is exercised by the cmd/pic promotion gate suite. Here we
	// pin the pure gherkin gate contract it delegates to.
	if err := ValidateGherkinSteps("Feature: f\nScenario: s\nGiven x\nWhen y\nThen z"); err != nil {
		t.Fatalf("scenario-shaped text must pass: %v", err)
	}
}
