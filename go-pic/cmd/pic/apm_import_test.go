package main

import (
	"database/sql"
	"encoding/json"
	"github.com/earendil-works/task-system/go-pic/internal/apm"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validTasksMD = `# Tasks: SampleFeature

**Feature:** demo/SampleFeature
**Plan:** .apm/specs/demo/SampleFeature.plan.md
**Date:** 2026-09-06
**Status:** Approved

## Format: [TID] [P?] [Priority] [US?] Description

## Scenario Map

| US | Scenario (.feature) | Tier |
|----|--------------------|------|
| US1 | Alpha works | @P1 |
| US2 | Alpha fails cleanly | @P1 |
| US3 | Beta controls | @P2 |
| US4 | Gamma polish | @P3 |

---

## Phase 1: Setup (parallelizable)

- [T001] [P] Setup: Create test helpers.
  - Files: ` + "`cmd/demo/demo_test.go`" + `
  - Trace: Plan Architecture.
  - Acceptance: ` + "`go test ./... -run TestDemoFixture` runs" + `.

- [T002] [P] Setup: Wire dispatch stub.
  - Files: ` + "`cmd/demo/demo.go`" + `
  - Trace: Plan Architecture.
  - Acceptance: ` + "`go test ./... -run TestDemoDispatch` passes" + `.

---

## Phase 2: Foundation (sequential after Setup)

- [T003] RED: Write failing parser tests.
  - Files: ` + "`cmd/demo/parse_test.go`" + `
  - Trace: Plan Architecture.
  - Acceptance: ` + "`go test ./... -run TestDemoParse` fails" + `.

- [T004] GREEN: Implement the parser.
  - Files: ` + "`cmd/demo/parse.go`" + `
  - Trace: Plan Architecture.
  - Acceptance: ` + "`go test ./... -run TestDemoParse` passes" + `.

- [T005] RED: Write failing validator tests.
  - Files: ` + "`cmd/demo/validate_test.go`" + `
  - Trace: Plan API Contract.
  - Acceptance: ` + "`go test ./... -run TestDemoValidate` fails" + `.

- [T006] GREEN: Implement the validator.
  - Files: ` + "`cmd/demo/validate.go`" + `
  - Trace: Plan API Contract.
  - Acceptance: ` + "`go test ./... -run TestDemoValidate` passes" + `.

---

## Phase 3: Priorities (per tier, grouped by scenario)

### P1: Critical Path

- [T007] [P1] [US1] RED: Write failing alpha happy-path test.
  - Files: ` + "`cmd/demo/alpha_test.go`" + `
  - Trace: Spec US1.
  - Acceptance: ` + "`go test ./... -run TestDemoAlpha` fails" + `.

- [T008] [P1] [US2] RED: Write failing alpha sad-path test.
  - Files: ` + "`cmd/demo/alpha_test.go`" + `
  - Trace: Spec US2.
  - Acceptance: ` + "`go test ./... -run TestDemoAlphaSad` fails" + `.

- [T009] [P1] [US1] [US2] GREEN: Implement alpha behavior.
  - Files: ` + "`cmd/demo/alpha.go`" + `
  - Trace: Spec US1/US2.
  - Acceptance: ` + "`go test ./... -run TestDemoAlpha` passes" + `.

### P2: Important

- [T010] [P2] [US3] RED: Write failing beta control test.
  - Files: ` + "`cmd/demo/beta_test.go`" + `
  - Trace: Spec US3.
  - Acceptance: ` + "`go test ./... -run TestDemoBeta` fails" + `.

- [T011] [P2] [US3] GREEN: Implement beta behavior.
  - Files: ` + "`cmd/demo/beta.go`" + `
  - Trace: Spec US3.
  - Acceptance: ` + "`go test ./... -run TestDemoBeta` passes" + `.

### P3+: Nice to Have

- [T012] [P3] [US4] RED: Write failing gamma test.
  - Files: ` + "`cmd/demo/gamma_test.go`" + `
  - Trace: Spec US4.
  - Acceptance: ` + "`go test ./... -run TestDemoGamma` fails" + `.

- [T013] [P3] [US4] GREEN: Implement gamma behavior.
  - Files: ` + "`cmd/demo/gamma.go`" + `
  - Trace: Spec US4.
  - Acceptance: ` + "`go test ./... -run TestDemoGamma` passes" + `.

---

## Phase 4: Polish (parallelizable)

- [T014] [P] REFACTOR: Clean up alpha module.
  - Files: ` + "`cmd/demo/alpha.go`" + `
  - Trace: Plan Defensive Coding.
  - Acceptance: ` + "`go test ./...` passes" + `.

- [T015] [P] Docs: Update module docs.
  - Files: ` + "`cmd/demo/README.md`" + `
  - Trace: Plan Docs.
  - Acceptance: ` + "`docs mention alpha`" + `.

---

## Phase 5: Final Verification (sequential)

- [T016] Run the full test suite.
  - Acceptance: ` + "`go test ./...` passes" + `.

- [T017] Verify lint.
  - Acceptance: ` + "`golangci-lint run` passes" + `.

- [T018] Verify build.
  - Acceptance: ` + "`go build ./...` passes" + `.

---

## Nyquist Mapping

| Requirement | Source | Verifying task | Verification command | Status |
|-------------|--------|----------------|----------------------|--------|
| R01 Demo requirement one | Plan API Contract | T007/T009/T016 | go test ./... -run TestDemoAlpha | Covered |
| R02 Demo requirement two | Plan Data Model | T012/T017 | go test ./... -run TestDemoGamma | Covered |

## Nyquist Result

Total requirements: 2
Covered: 2
Result: PASS — no verification debt registered.

## Execution Order

` + "```text" + `
Phase 1: T001 ║ T002
           ↓
Phase 2: T003 → T004 → T005 → T006
           ↓
Phase 3 P1: T007 → T008 → T009
           ↓
Phase 3 P2: T010 → T011
           ↓
Phase 3 P3: T012 → T013
           ↓
Phase 4: T014 → T015
           ↓
Phase 5: T016 → T017 → T018
` + "```" + `

**Legend:** ` + "`→` sequential | `║` parallel"

const companionPlanMD = `# Technical Blueprint: SampleFeature

**Spec:** .apm/specs/demo/SampleFeature.feature
**Date:** 2026-09-06
**Status:** Approved
`

const companionFeatureMD = `# .apm/specs/demo/SampleFeature.feature
# Status: @ready

@ready
Feature: SampleFeature
  As an owner
  I want alpha beta gamma
  So that demo works

  @P1 @US1
  Scenario: Alpha works
    Given alpha
    When alpha runs
    Then it works

  ## Priority P1: Critical Path

  @P1 @US2
  Scenario: Alpha fails cleanly
    Given broken alpha
    When alpha runs
    Then it fails cleanly

  @P2 @US3
  Scenario: Beta controls
    Given beta
    When beta runs
    Then it controls

  @P3 @US4
  Scenario: Gamma polish
    Given gamma
    When gamma runs
    Then it polishes

# === SUCCESS CRITERIA ===
# UX: everything works
`

func writeApmProject(t *testing.T) (bin string, root string, home string) {
	t.Helper()
	bin, root, home = writeApmProjectWithTasks(t, validTasksMD)
	return bin, root, home
}

func writeApmProjectWithTasks(t *testing.T, tasksMD string) (bin string, root string, home string) {
	t.Helper()
	bin = buildPic(t)
	root, home = initProject(t, bin)
	dir := filepath.Join(root, ".apm", "specs", "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"SampleFeature.tasks.md": tasksMD,
		"SampleFeature.plan.md":  companionPlanMD,
		"SampleFeature.feature":  companionFeatureMD,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return bin, root, home
}

func TestApmImportFixture(t *testing.T) {
	t.Parallel()
	doc, err := apm.ParseTasksMD(validTasksMD)
	if err != nil {
		t.Fatalf("parse valid fixture: %v", err)
	}
	if doc.Name != "SampleFeature" {
		t.Fatalf("doc.Name = %q", doc.Name)
	}
	if doc.Status != "Approved" {
		t.Fatalf("doc.Status = %q", doc.Status)
	}
	if doc.PlanPath != ".apm/specs/demo/SampleFeature.plan.md" {
		t.Fatalf("doc.PlanPath = %q", doc.PlanPath)
	}
	if len(doc.Tasks) != 18 {
		t.Fatalf("len(doc.Tasks) = %d, want 18", len(doc.Tasks))
	}
	if len(doc.ScenarioUS) != 4 {
		t.Fatalf("scenario map US count = %d, want 4", len(doc.ScenarioUS))
	}
	byID := map[string]apm.Task{}
	for _, task := range doc.Tasks {
		byID[task.TID] = task
	}
	if !byID["T001"].Parallel || !byID["T002"].Parallel {
		t.Fatalf("setup tasks parallel flags = %#v %#v", byID["T001"].Parallel, byID["T002"].Parallel)
	}
	if byID["T001"].Phase != 1 || byID["T003"].Phase != 2 || byID["T007"].Phase != 3 || byID["T014"].Phase != 4 || byID["T016"].Phase != 5 {
		t.Fatalf("phase assignment = %#v", byID)
	}
	if byID["T003"].Tier != "" {
		t.Fatalf("foundation task tier = %q, want empty", byID["T003"].Tier)
	}
	if byID["T007"].Tier != "P1" || byID["T010"].Tier != "P2" || byID["T012"].Tier != "P3" {
		t.Fatalf("tier tags = %#v", byID)
	}
	if byID["T007"].US != "US1" || byID["T009"].US != "US1 US2" {
		t.Fatalf("US tags = %q / %q", byID["T007"].US, byID["T009"].US)
	}
	if !strings.Contains(byID["T007"].Verbatim, "- [T007] [P1] [US1] RED: Write failing alpha happy-path test.") {
		t.Fatalf("verbatim block missing task line: %q", byID["T007"].Verbatim)
	}
	if !strings.Contains(byID["T007"].Acceptance, "go test ./... -run TestDemoAlpha` fails") {
		t.Fatalf("acceptance line = %q", byID["T007"].Acceptance)
	}
	if len(doc.TierHeaders) != 3 || doc.TierHeaders["P1"] != "P1: Critical Path" || doc.TierHeaders["P2"] != "P2: Important" || doc.TierHeaders["P3"] != "P3+: Nice to Have" {
		t.Fatalf("tier headers = %#v", doc.TierHeaders)
	}
	wantOrder := []struct {
		phase    int
		tier     string
		seq      []string
		parallel bool
	}{
		{1, "", []string{"T001", "T002"}, true},
		{2, "", []string{"T003", "T004", "T005", "T006"}, false},
		{3, "P1", []string{"T007", "T008", "T009"}, false},
		{3, "P2", []string{"T010", "T011"}, false},
		{3, "P3", []string{"T012", "T013"}, false},
		{4, "", []string{"T014", "T015"}, false},
		{5, "", []string{"T016", "T017", "T018"}, false},
	}
	if len(doc.Order) != len(wantOrder) {
		t.Fatalf("order lines = %#v", doc.Order)
	}
	for i, want := range wantOrder {
		got := doc.Order[i]
		if got.Phase != want.phase || got.Tier != want.tier || got.Parallel != want.parallel {
			t.Fatalf("order line %d = %#v, want phase %d tier %q parallel %v", i, got, want.phase, want.tier, want.parallel)
		}
		if strings.Join(got.Seq, ",") != strings.Join(want.seq, ",") {
			t.Fatalf("order line %d seq = %v, want %v", i, got.Seq, want.seq)
		}
	}
}

func TestApmImportRuleD(t *testing.T) {
	t.Parallel()
	base, err := apm.ParseTasksMD(validTasksMD)
	if err != nil {
		t.Fatal(err)
	}
	if diffs := apm.ValidateTasks(base); len(diffs) != 0 {
		t.Fatalf("valid fixture discrepancies = %v", diffs)
	}

	cases := []struct {
		name    string
		mutate  func(string) string
		wantSub string
	}{
		{
			name: "block references missing TID",
			mutate: func(s string) string {
				return strings.Replace(s, "Phase 4: T014 → T015", "Phase 4: T014 → T015 → T099", 1)
			},
			wantSub: "T099",
		},
		{
			name: "duplicate task in block",
			mutate: func(s string) string {
				return strings.Replace(s, "Phase 2: T003 → T004 → T005", "Phase 2: T003 → T004 → T004 → T005", 1)
			},
			wantSub: "T004",
		},
		{
			name:    "parallel pair without [P] marker",
			mutate:  func(s string) string { return strings.Replace(s, "- [T002] [P] Setup:", "- [T002] Setup:", 1) },
			wantSub: "[P]",
		},
		{
			name:    "US tag absent from Scenario Map",
			mutate:  func(s string) string { return strings.Replace(s, "- [T007] [P1] [US1]", "- [T007] [P1] [US9]", 1) },
			wantSub: "US9",
		},
		{
			name:    "missing tier header",
			mutate:  func(s string) string { return strings.Replace(s, "### P2: Important", "### P2 Renamed", 1) },
			wantSub: "P2",
		},
		{
			name:    "task missing from block",
			mutate:  func(s string) string { return strings.Replace(s, "Phase 3 P3: T012 → T013", "Phase 3 P3: T012", 1) },
			wantSub: "T013",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := apm.ParseTasksMD(tc.mutate(validTasksMD))
			if err != nil {
				t.Fatalf("parse corrupted fixture: %v", err)
			}
			diffs := apm.ValidateTasks(doc)
			if len(diffs) == 0 {
				t.Fatalf("expected discrepancies, got none")
			}
			found := false
			for _, d := range diffs {
				if strings.Contains(d, tc.wantSub) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("discrepancies %v do not mention %q", diffs, tc.wantSub)
			}
		})
	}
}

func TestApmImportDryRun(t *testing.T) {
	t.Parallel()
	bin, root, home := writeApmProject(t)
	out := runPic(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0", "--dry-run")
	graph := asObject(t, out)
	epic := asObject(t, graph["epic"])
	if epic["name"] != "SampleFeature" {
		t.Fatalf("epic name = %#v", epic["name"])
	}
	if epic["milestone_label"] != "milestone:v1.0" {
		t.Fatalf("milestone label = %#v", epic["milestone_label"])
	}
	if len(asArray(t, epic["verification_commands"])) != 3 {
		t.Fatalf("verification commands = %#v", epic["verification_commands"])
	}
	features := asArray(t, graph["features"])
	if len(features) != 3 {
		t.Fatalf("features = %#v", features)
	}
	want := []struct{ key, name, tasks string }{
		{"F1", "P1: Critical Path", "T001,T002,T003,T004,T005,T006,T007,T008,T009"},
		{"F2", "P2: Important", "T010,T011"},
		{"F3", "P3+: Nice to Have", "T012,T013,T014,T015"},
	}
	for i, f := range features {
		obj := asObject(t, f)
		if obj["key"] != want[i].key || obj["name"] != want[i].name {
			t.Fatalf("feature %d = %#v", i, obj)
		}
		got := ""
		for _, tid := range asArray(t, obj["task_ids"]) {
			got += tid.(string) + ","
		}
		if got != want[i].tasks+"," {
			t.Fatalf("feature %s task_ids = %q", want[i].key, got)
		}
	}
	tasks := asArray(t, graph["tasks"])
	if len(tasks) != 15 {
		t.Fatalf("tasks = %d, want 15 (Phase 5 excluded)", len(tasks))
	}
	first := asObject(t, tasks[0])
	if first["tid"] != "T001" || first["feature"] != "F1" || first["depends_on"] != nil {
		t.Fatalf("first task = %#v", first)
	}
	// Rule C: the fixture's T001 ║ T002 pair must share an empty predecessor
	// set — a declared-parallel pair must not be serialized by the flattened
	// execution order.
	second := asObject(t, tasks[1])
	if second["tid"] != "T002" {
		t.Fatalf("second task = %#v", second)
	}
	if dep, ok := second["depends_on"].([]any); ok && len(dep) != 0 {
		t.Fatalf("parallel task T002 depends_on = %#v, want empty", second["depends_on"])
	}
	edges := asArray(t, graph["edges"])
	if len(edges) != 2 {
		t.Fatalf("feature edges = %#v", edges)
	}
	e0 := asObject(t, edges[0])
	e1 := asObject(t, edges[1])
	if e0["from"] != "F2" || e0["to"] != "F1" || e1["from"] != "F3" || e1["to"] != "F2" {
		t.Fatalf("feature edges = %#v", edges)
	}

	// Zero writes: store unchanged.
	listed := runPic(t, bin, root, home, "work-item", "list").([]any)
	if len(listed) != 0 {
		t.Fatalf("dry-run wrote rows: %#v", listed)
	}

	// Deterministic across runs.
	out2 := runPic(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0", "--dry-run")
	if fmtJSON(t, out) != fmtJSON(t, out2) {
		t.Fatalf("dry-run output differs between runs")
	}
}

func TestApmImportAbort(t *testing.T) {
	t.Parallel()
	corrupt := strings.Replace(validTasksMD, "Phase 4: T014 → T015", "Phase 4: T014 → T015 → T099", 1)
	bin, root, home := writeApmProjectWithTasks(t, corrupt)
	out := runPicError(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0")
	if !strings.Contains(out, "T099") {
		t.Fatalf("abort error missing discrepancy: %s", out)
	}
	if !strings.Contains(out, "imported") || !strings.Contains(out, "false") {
		t.Fatalf("abort error missing imported=false: %s", out)
	}
	listed := runPic(t, bin, root, home, "work-item", "list").([]any)
	if len(listed) != 0 {
		t.Fatalf("abort wrote rows: %#v", listed)
	}
}

func TestApmImportAbortNotApproved(t *testing.T) {
	t.Parallel()
	draft := strings.Replace(validTasksMD, "**Status:** Approved", "**Status:** Draft", 1)
	bin, root, home := writeApmProjectWithTasks(t, draft)
	out := runPicError(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0")
	if !strings.Contains(out, "Approved") {
		t.Fatalf("gate error = %s", out)
	}
	listed := runPic(t, bin, root, home, "work-item", "list").([]any)
	if len(listed) != 0 {
		t.Fatalf("gate wrote rows: %#v", listed)
	}
}

func TestApmImportCreatesWorkItems(t *testing.T) {
	t.Parallel()
	bin, root, home := writeApmProject(t)
	result := asObject(t, runPic(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0"))
	if result["imported"] != true {
		t.Fatalf("result = %#v", result)
	}
	epicID := result["epic_id"].(string)
	listed := runPic(t, bin, root, home, "work-item", "list").([]any)
	if len(listed) != 19 {
		t.Fatalf("work items = %d, want 19 (epic + 3 features + 15 tasks)", len(listed))
	}

	epic := asObject(t, runPic(t, bin, root, home, "work-item", "show", epicID))
	if epic["type"] != "epic" || epic["parent_id"] != nil {
		t.Fatalf("epic = %#v", epic)
	}
	features := runPic(t, bin, root, home, "work-item", "list", "--label", "apm-feature").([]any)
	if len(features) != 3 {
		t.Fatalf("features by label = %#v", features)
	}
	// Task description carries verbatim block + Acceptance as criteria.
	tasks := runPic(t, bin, root, home, "work-item", "list", "--label", "apm-task").([]any)
	if len(tasks) != 15 {
		t.Fatalf("tasks by label = %d", len(tasks))
	}
	var alpha map[string]any
	for i := range tasks {
		obj := asObject(t, tasks[i])
		if obj["title"] == "RED: Write failing alpha happy-path test." {
			alpha = obj
		}
	}
	if alpha == nil {
		t.Fatalf("alpha task not found in %#v", tasks)
	}
	desc := alpha["description"].(string)
	if !strings.Contains(desc, "- [T007] [P1] [US1] RED: Write failing alpha happy-path test.") {
		t.Fatalf("task description missing verbatim block: %s", desc)
	}
	if !strings.Contains(desc, "go test ./... -run TestDemoAlpha` fails") {
		t.Fatalf("task description missing acceptance: %s", desc)
	}
	_ = epicID
	_ = epic
}

func asArray(t *testing.T, value any) []any {
	t.Helper()
	arr, ok := value.([]any)
	if !ok {
		t.Fatalf("expected array, got %#v", value)
	}
	return arr
}

func fmtJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApmImportGates(t *testing.T) {
	t.Parallel()
	t.Run("missing companion plan", func(t *testing.T) {
		bin, root, home := writeApmProject(t)
		if err := os.Remove(filepath.Join(root, ".apm", "specs", "demo", "SampleFeature.plan.md")); err != nil {
			t.Fatal(err)
		}
		out := runPicError(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0")
		if !strings.Contains(out, "missing") {
			t.Fatalf("gate error = %s", out)
		}
		if listed := runPic(t, bin, root, home, "work-item", "list").([]any); len(listed) != 0 {
			t.Fatalf("gate wrote rows: %#v", listed)
		}
	})
	t.Run("unready companion feature", func(t *testing.T) {
		bin, root, home := writeApmProject(t)
		feat := filepath.Join(root, ".apm", "specs", "demo", "SampleFeature.feature")
		content := strings.Replace(mustRead(t, feat), "# Status: @ready", "# Status: @draft", 1)
		if err := os.WriteFile(feat, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		out := runPicError(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0")
		if !strings.Contains(out, "@ready") {
			t.Fatalf("gate error = %s", out)
		}
		if listed := runPic(t, bin, root, home, "work-item", "list").([]any); len(listed) != 0 {
			t.Fatalf("gate wrote rows: %#v", listed)
		}
	})
}

func TestApmImportReimport(t *testing.T) {
	t.Parallel()
	bin, root, home := writeApmProject(t)
	first := asObject(t, runPic(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0"))
	out := runPicError(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0")
	if !strings.Contains(out, "already imported") || !strings.Contains(out, first["epic_id"].(string)) {
		t.Fatalf("re-import error = %s", out)
	}
	if listed := runPic(t, bin, root, home, "work-item", "list").([]any); len(listed) != 19 {
		t.Fatalf("re-import changed store: %d rows", len(listed))
	}
}

func TestApmImportNoCrossFeatureEdges(t *testing.T) {
	t.Parallel()
	bin, root, home := writeApmProject(t)
	runPic(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0")
	// Feature parent ids: F2/F3 tasks must not depend on any F1 task.
	all := runPic(t, bin, root, home, "work-item", "list").([]any)
	parentOf := map[string]string{}
	isTask := map[string]bool{}
	for i := range all {
		obj := asObject(t, all[i])
		parentOf[obj["id"].(string)] = ""
		if v, ok := obj["parent_id"].(string); ok {
			parentOf[obj["id"].(string)] = v
		}
		isTask[obj["id"].(string)] = obj["type"] == "task"
	}
	// No cross-feature dependency rows: for every dependency, both ends share a parent.
	rows := queryDependendencies(t, bin, root, home)
	featureEdges := 0
	for _, row := range rows {
		from, to := row[0], row[1]
		fp, tp := parentOf[from], parentOf[to]
		if !isTask[from] {
			// feature-level edge: both ends are features parented by the epic
			if fp == "" || fp != tp {
				t.Fatalf("feature edge does not chain siblings: %v", row)
			}
			featureEdges++
			continue
		}
		if tp == "" || fp != tp {
			t.Fatalf("cross-feature dependency: %v", row)
		}
	}
	if featureEdges != 2 {
		t.Fatalf("feature edges = %d, want 2", featureEdges)
	}
}

func TestApmImportPhase5Epic(t *testing.T) {
	t.Parallel()
	bin, root, home := writeApmProject(t)
	result := asObject(t, runPic(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0"))
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "show", result["epic_id"].(string)))
	desc := epic["description"].(string)
	if !strings.Contains(desc, "Nyquist Mapping") || !strings.Contains(desc, "R01 Demo requirement one") {
		t.Fatalf("epic description missing Nyquist table: %s", desc)
	}
	if !strings.Contains(desc, "T016") || !strings.Contains(desc, "T017") || !strings.Contains(desc, "T018") {
		t.Fatalf("epic description missing Phase 5 commands: %s", desc)
	}
	tasks := runPic(t, bin, root, home, "work-item", "list", "--label", "apm-task").([]any)
	for i := range tasks {
		title := asObject(t, tasks[i])["title"].(string)
		if strings.Contains(title, "Run the full test suite") || strings.Contains(title, "Verify lint") || strings.Contains(title, "Verify build") {
			t.Fatalf("Phase 5 task became a Work Item: %s", title)
		}
	}
}

func TestApmImportLabels(t *testing.T) {
	t.Parallel()
	bin, root, home := writeApmProject(t)
	result := asObject(t, runPic(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0"))
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "show", result["epic_id"].(string)))
	labels := epic["labels"].([]any)
	if len(labels) != 2 || labels[0] != "import:" && labels[1] != "milestone:v1.0" && labels[0] != "milestone:v1.0" && labels[1] != "import:" {
		if !(len(labels) == 2 && store.Contains([]string{labels[0].(string), labels[1].(string)}, "milestone:v1.0") && strings.HasPrefix(altLabel(labels), "import:")) {
			t.Fatalf("epic labels = %#v", labels)
		}
	}
}

func TestApmImportNoGit(t *testing.T) {
	t.Parallel()
	// initProject creates no git repository; import must succeed without one.
	bin, root, home := writeApmProject(t)
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		t.Fatalf("test project unexpectedly has .git")
	}
	runPic(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0")
}

func TestWorkflowCommandDispatch(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	out := runPicError(t, bin, root, home, "workflow", "import-apm")
	if !strings.Contains(out, "usage") {
		t.Fatalf("usage error = %s", out)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func queryDependendencies(t *testing.T, bin, root, home string) [][2]string {
	t.Helper()
	_ = bin
	_ = home
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT work_item_id,depends_on_work_item_id FROM work_item_dependencies ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			t.Fatal(err)
		}
		out = append(out, [2]string{from, to})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func altLabel(labels []any) string {
	for _, l := range labels {
		if strings.HasPrefix(l.(string), "import:") {
			return l.(string)
		}
	}
	return ""
}

func TestApmImportGherkinEmbed(t *testing.T) {
	t.Parallel()
	// Pillar 4: every US-tagged task carries its verbatim Gherkin scenario
	// from the companion .feature; untagged tasks carry none.
	bin, root, home := writeApmProject(t)
	out := runPic(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0")
	_ = out
	db, err := sql.Open("sqlite", filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT title,description FROM work_items WHERE type='task'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var title, desc string
		if err := rows.Scan(&title, &desc); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(title, "alpha happy-path") {
			found++
			for _, want := range []string{"Behavior context (US1)", "Scenario: Alpha works", "Given alpha", "Then it works"} {
				if !strings.Contains(desc, want) {
					t.Fatalf("US1 task description missing %q: %s", want, desc)
				}
			}
			if strings.Contains(desc, "## Priority") {
				t.Fatalf("US1 scenario body leaked a section heading: %s", desc)
			}
			if strings.Contains(desc, "SUCCESS CRITERIA") {
				t.Fatalf("scenario body leaked trailing footer blocks: %s", desc)
			}
		}
		if strings.Contains(title, "GREEN: Implement gamma") {
			if strings.Contains(desc, "SUCCESS CRITERIA") {
				t.Fatalf("last scenario body leaked trailing footer blocks: %s", desc)
			}
		}
		if strings.Contains(title, "Setup: Create test helpers") && strings.Contains(desc, "Behavior context") {
			t.Fatalf("untagged setup task carries scenario context: %s", desc)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if found != 1 {
		t.Fatalf("expected exactly 1 alpha happy-path task, got %d", found)
	}
}

func TestApmImportRuleDScenarioGaps(t *testing.T) {
	t.Parallel()
	// Fail closed: a scenario missing its @US tag, or a Scenario Map US with
	// no tagged scenario, aborts the import.
	cases := map[string]string{
		"untagged scenario":   "  @P1\n  Scenario: Alpha works",
		"missing US2 mapping": "  @P1\n  Scenario: Alpha fails cleanly",
	}
	for name, replacement := range cases {
		t.Run(name, func(t *testing.T) {
			feat := strings.Replace(companionFeatureMD, "  @P1 @US1\n  Scenario: Alpha works", replacement, 1)
			bin, root, home := writeApmProjectWithTasks(t, validTasksMD)
			featPath := filepath.Join(root, ".apm", "specs", "demo", "SampleFeature.feature")
			if err := os.WriteFile(featPath, []byte(feat), 0o644); err != nil {
				t.Fatal(err)
			}
			out := runPicError(t, bin, root, home, "workflow", "import-apm", ".apm/specs/demo/SampleFeature.tasks.md", "--milestone", "v1.0")
			if !strings.Contains(out, "Execution Order block is inconsistent") && !strings.Contains(out, "scenario") && !strings.Contains(out, "Scenario") {
				t.Fatalf("expected scenario gap abort, got: %s", out)
			}
		})
	}
}
