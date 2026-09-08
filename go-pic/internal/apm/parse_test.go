package apm

import (
	"strings"
	"testing"
)

const sampleTasksMD = `# Tasks: Demo Epic

**Feature:** .feature
**Plan:** .plan.md
**Status:** Approved

## Phase 1

- [T001] [US1] Do the thing
  - Files: a.go
  - Trace: R01
  - Acceptance: Given a thing
When it runs
Then it works

## Execution Order

Phase 1: T001

## Nyquist Mapping

| US | Source | Task | Command |
| -- | ------ | ---- | ------- |
| US1 | R01 | T001 | go test | 
`

func TestParseTasksMD(t *testing.T) {
	t.Parallel()
	doc, err := ParseTasksMD(sampleTasksMD)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "Demo Epic" {
		t.Errorf("title = %q", doc.Name)
	}
	if len(doc.Tasks) != 1 || doc.Tasks[0].TID != "T001" {
		t.Fatalf("tasks = %#v", doc.Tasks)
	}
	if doc.Tasks[0].US != "US1" {
		t.Errorf("task US = %q", doc.Tasks[0].US)
	}
	if len(doc.Order) != 1 || doc.Order[0].Phase != 1 || len(doc.Order[0].Seq) != 1 {
		t.Errorf("order = %#v", doc.Order)
	}
}

func TestValidateTasks(t *testing.T) {
	t.Parallel()
	doc, err := ParseTasksMD(sampleTasksMD)
	if err != nil {
		t.Fatal(err)
	}
	// A minimal single-phase doc cannot satisfy the full cross-phase tier
	// rules; pin the discrepancies it does produce for the covered rules.
	got := ValidateTasks(doc)
	joined := strings.Join(got, ";")
	for _, want := range []string{"T001 references US1 which is absent from the Scenario Map", "task T001 is in Phase 0 but the block places it in Phase 1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("discrepancies %q missing %q", joined, want)
		}
	}
	// duplicate id in the order block
	duplicated := strings.Replace(sampleTasksMD, "Phase 1: T001", "Phase 1: T001 T001", 1)
	dupDoc, err := ParseTasksMD(duplicated)
	if err != nil {
		t.Fatal(err)
	}
	dupJoined := strings.Join(ValidateTasks(dupDoc), ";")
	if !strings.Contains(dupJoined, "task T001 is missing from the Execution Order block") {
		t.Fatalf("duplicated order discrepancies = %v", dupJoined)
	}
	// ghost reference
	ghostDoc, err := ParseTasksMD(strings.Replace(sampleTasksMD, "Phase 1: T001", "Phase 1: T999", 1))
	if err != nil {
		t.Fatal(err)
	}
	got = ValidateTasks(ghostDoc)
	if len(got) == 0 || !strings.Contains(strings.Join(got, ";"), "T999") {
		t.Fatalf("ghost discrepancies = %v", got)
	}
}

func TestTaskTitle(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"- [T001] [US1] [P1] Do the thing": "Do the thing",
		"- [T002] Plain task":              "Plain task",
		"[T003] no tag":                    "no tag",
	}
	for input, want := range tests {
		if got := TaskTitle(input); got != want {
			t.Errorf("TaskTitle(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildGraph(t *testing.T) {
	t.Parallel()
	doc, err := ParseTasksMD(sampleTasksMD)
	if err != nil {
		t.Fatal(err)
	}
	graph := BuildGraph(doc, "v1.0")
	if graph.MilestoneLabel != "milestone:v1.0" {
		t.Errorf("milestone label = %q", graph.MilestoneLabel)
	}
	if graph.EpicName != "Demo Epic" {
		t.Errorf("epic name = %q", graph.EpicName)
	}
	if len(graph.Features) != 1 || graph.Features[0].Key != "F1" || len(graph.Features[0].TaskTIDs) != 1 {
		t.Fatalf("features = %#v", graph.Features)
	}
	if len(graph.Tasks) != 1 || graph.Tasks[0].TID != "T001" {
		t.Fatalf("tasks = %#v", graph.Tasks)
	}
}

func TestShortHash(t *testing.T) {
	t.Parallel()
	first := ShortHash([]byte("content"))
	second := ShortHash([]byte("content"))
	if first != second || len(first) != 12 {
		t.Fatalf("hash = %q, want stable 12 chars", first)
	}
	if ShortHash([]byte("other")) == first {
		t.Error("different content must hash differently")
	}
}
