package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// APM import: deterministic parser for owner-approved .tasks.md files.
// The .tasks.md is the planning source of truth; this importer is the only
// path that converts it into canonical Work Items (supersedes the legacy
// scan→RRI→vision→blueprint→contracts→task-graph→TIP planning pipeline).

type apmTask struct {
	TID        string
	Phase      int
	Tier       string // "P1"/"P2"/"P3" inside Phase 3, empty otherwise
	US         string // space-separated US tags from the task header, empty for setup/polish
	Parallel   bool   // carries the [P] marker
	Title      string // task description on the [TID] line, minus tags
	Verbatim   string // full task block, verbatim
	Acceptance string
}

type apmOrderLine struct {
	Phase    int
	Tier     string
	Seq      []string
	Parallel bool
}

type apmDoc struct {
	Name        string
	Status      string
	PlanPath    string
	ScenarioUS  []string
	TierHeaders map[string]string // "P1" -> "P1: Critical Path" (verbatim header text)
	Tasks       []apmTask
	Order       []apmOrderLine
	// Scenarios maps US id -> verbatim Gherkin scenario block (tagged @US<n>)
	// parsed from the companion .feature; embedded into task descriptions as
	// the behavioral context pillar.
	Scenarios map[string]string
	// NyquistSection carries the verbatim "## Nyquist Mapping" markdown body
	// (through "## Nyquist Result") so the epic description attests the
	// requirement-to-verification mapping from the single approved artifact.
	NyquistSection string
}

var (
	apmHeaderRe      = regexp.MustCompile(`^# Tasks: (.+)$`)
	apmFieldRe       = regexp.MustCompile(`^\*\*(Feature|Plan|Status):\*\* (.+)$`)
	apmPhaseRe       = regexp.MustCompile(`^## Phase (\d+):`)
	apmTierRe        = regexp.MustCompile(`^### (P[123]\+?):\s*(.+)$`)
	apmTaskRe        = regexp.MustCompile(`^- \[(T\d{3})\]\s*(.*)$`)
	apmDetailRe      = regexp.MustCompile(`^  - (Files|Trace|Acceptance): (.*)$`)
	apmScenarioUSRe  = regexp.MustCompile(`^\| (US\d+) \|`)
	apmOrderLineRe   = regexp.MustCompile(`^Phase (\d+)(?:\s+(P[123]\+?))?:\s*(.+)$`)
	apmTaskTagUSRe   = regexp.MustCompile(`\[US(\d+)\]`)
	apmTaskTagTierRe = regexp.MustCompile(`\[(P[123])\]`)
)

// parseApmTasksMD converts .tasks.md text into the import document. It never
// infers: whatever is not in the file is absent from the doc.
func parseApmTasksMD(content string) (*apmDoc, error) {
	doc := &apmDoc{TierHeaders: map[string]string{}}
	lines := strings.Split(content, "\n")
	phase := 0
	tier := ""
	inOrder := false
	var task *apmTask
	scenarioUSSeen := map[string]bool{}

	flush := func() {
		if task != nil {
			doc.Tasks = append(doc.Tasks, *task)
			task = nil
		}
	}

	inNyquist := false
	var nyquist strings.Builder
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			flush()
			inNyquist = trimmed == "## Nyquist Mapping"
			if inNyquist {
				nyquist.WriteString(line)
				nyquist.WriteString("\n")
				continue
			}
		}
		if inNyquist {
			nyquist.WriteString(line)
			nyquist.WriteString("\n")
			continue
		}
		if m := apmHeaderRe.FindStringSubmatch(trimmed); m != nil && doc.Name == "" {
			doc.Name = m[1]
			continue
		}
		if m := apmFieldRe.FindStringSubmatch(trimmed); m != nil {
			switch m[1] {
			case "Plan":
				doc.PlanPath = strings.TrimSpace(m[2])
			case "Status":
				doc.Status = strings.TrimSpace(m[2])
			}
			continue
		}
		if m := apmPhaseRe.FindStringSubmatch(trimmed); m != nil {
			flush()
			fmt.Sscanf(m[1], "%d", &phase)
			tier = ""
			inOrder = false
			continue
		}
		if m := apmTierRe.FindStringSubmatch(trimmed); m != nil {
			tier = strings.TrimSuffix(m[1], "+")
			doc.TierHeaders[tier] = strings.TrimSpace(m[1] + ": " + m[2])
			continue
		}
		if m := apmScenarioUSRe.FindStringSubmatch(trimmed); m != nil {
			if !scenarioUSSeen[m[1]] {
				scenarioUSSeen[m[1]] = true
				doc.ScenarioUS = append(doc.ScenarioUS, m[1])
			}
			continue
		}
		if trimmed == "## Execution Order" {
			flush()
			inOrder = true
			continue
		}
		if inOrder {
			if m := apmOrderLineRe.FindStringSubmatch(trimmed); m != nil {
				ol := apmOrderLine{Tier: strings.TrimSuffix(m[2], "+")}
				fmt.Sscanf(m[1], "%d", &ol.Phase)
				for _, part := range regexp.MustCompile(`\s*(→|║)\s*`).Split(m[3], -1) {
					if id := strings.TrimSpace(part); id != "" {
						ol.Seq = append(ol.Seq, id)
					}
				}
				ol.Parallel = strings.Contains(m[3], "║")
				doc.Order = append(doc.Order, ol)
			}
			continue
		}
		if m := apmTaskRe.FindStringSubmatch(line); m != nil {
			flush()
			nt := apmTask{TID: m[1], Phase: phase, Tier: tier, Title: strings.TrimSpace(m[2]), Verbatim: line}
			if strings.Contains(m[2], "[P]") {
				nt.Parallel = true
			}
			for _, us := range apmTaskTagUSRe.FindAllStringSubmatch(m[2], -1) {
				nt.US = strings.TrimSpace(nt.US + " US" + us[1])
			}
			if mt := apmTaskTagTierRe.FindStringSubmatch(m[2]); mt != nil {
				nt.Tier = mt[1]
			}
			task = &nt
			continue
		}
		if task != nil {
			if m := apmDetailRe.FindStringSubmatch(line); m != nil {
				switch m[1] {
				case "Acceptance":
					task.Acceptance = strings.TrimSpace(m[2])
				}
				task.Verbatim += "\n" + line
			} else if trimmed != "" && trimmed != "---" && !strings.HasPrefix(trimmed, "##") && !strings.HasPrefix(trimmed, "###") {
				task.Verbatim += "\n" + line
			}
		}
	}
	flush()

	if doc.Name == "" {
		return nil, fmt.Errorf("no `# Tasks: <Name>` header found")
	}
	if len(doc.Tasks) == 0 {
		return nil, fmt.Errorf("no tasks found")
	}
	if len(doc.Order) == 0 {
		return nil, fmt.Errorf("no Execution Order block found")
	}
	doc.NyquistSection = strings.TrimRight(nyquist.String(), "\n")
	return doc, nil
}

// validateApmTasks implements Rule D: the Execution Order block must agree
// with the task list exactly — every mismatch is reported, none is repaired.
func validateApmTasks(doc *apmDoc) []string {
	var diffs []string
	byID := map[string]apmTask{}
	for _, t := range doc.Tasks {
		if _, dup := byID[t.TID]; dup {
			diffs = append(diffs, fmt.Sprintf("duplicate task id %s in task list", t.TID))
		}
		byID[t.TID] = t
	}

	ordered := map[string]int{}
	for _, ol := range doc.Order {
		for _, tid := range ol.Seq {
			if _, dup := ordered[tid]; dup {
				diffs = append(diffs, fmt.Sprintf("task %s appears more than once in the Execution Order block", tid))
				continue
			}
			ordered[tid] = ol.Phase
			t, ok := byID[tid]
			if !ok {
				diffs = append(diffs, fmt.Sprintf("Execution Order block references %s which does not exist in the task list", tid))
				continue
			}
			if t.Phase != ol.Phase {
				diffs = append(diffs, fmt.Sprintf("task %s is in Phase %d but the block places it in Phase %d", tid, t.Phase, ol.Phase))
			}
			if t.Tier != ol.Tier {
				diffs = append(diffs, fmt.Sprintf("task %s tier %q does not match block tier %q", tid, t.Tier, ol.Tier))
			}
		}
		if ol.Parallel {
			for _, tid := range ol.Seq {
				if t, ok := byID[tid]; ok && !t.Parallel {
					diffs = append(diffs, fmt.Sprintf("parallel pair in the Execution Order block requires task %s to carry the [P] marker", tid))
				}
			}
		}
	}
	for _, t := range doc.Tasks {
		if _, ok := ordered[t.TID]; !ok {
			diffs = append(diffs, fmt.Sprintf("task %s is missing from the Execution Order block", t.TID))
		}
		if t.US != "" {
			for _, us := range strings.Fields(t.US) {
				if !scenarioUSPresent(doc, us) {
					diffs = append(diffs, fmt.Sprintf("task %s references %s which is absent from the Scenario Map", t.TID, us))
				}
			}
		}
	}
	for _, tier := range []string{"P1", "P2", "P3"} {
		if _, ok := doc.TierHeaders[tier]; !ok {
			diffs = append(diffs, fmt.Sprintf("Phase 3 is missing its %s tier header", tier))
		}
	}
	sort.Strings(diffs)
	return diffs
}

func scenarioUSPresent(doc *apmDoc, us string) bool {
	for _, known := range doc.ScenarioUS {
		if known == us {
			return true
		}
	}
	return false
}

type apmScenario struct {
	US   string
	Body string
}

// parseApmScenarios extracts verbatim Gherkin scenario blocks from a .feature
// keyed by the @US<n> tag directly above the Scenario: line (authoring rule:
// every Scenario carries exactly one @US tag).
func parseApmScenarios(featureMD string) []apmScenario {
	lines := strings.Split(featureMD, "\n")
	starts := map[int]string{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Scenario:") {
			continue
		}
		us := ""
		for j := i - 1; j >= 0; j-- {
			tag := strings.TrimSpace(lines[j])
			if !strings.HasPrefix(tag, "@") {
				break
			}
			for _, field := range strings.Fields(tag) {
				if strings.HasPrefix(field, "@US") {
					us = strings.TrimPrefix(field, "@")
				}
			}
			j--
		}
		starts[i] = us
	}
	idxs := make([]int, 0, len(starts))
	for i := range starts {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	var out []apmScenario
	for n, start := range idxs {
		end := len(lines)
		limit := end
		if n+1 < len(idxs) {
			limit = idxs[n+1]
		}
		// A scenario ends where the next scenario's tag block begins or at a
		// Gherkin section heading / column-0 footer comment, whichever comes first.
		for e := start + 1; e < limit; e++ {
			t := strings.TrimSpace(lines[e])
			// Scenario bodies span indented steps and indented comments only:
			// section headings, tag blocks, and column-0 comment footers
			// (e.g. "# === SUCCESS CRITERIA ===") all end the body.
			if strings.HasPrefix(t, "@") || strings.HasPrefix(t, "## ") ||
				(strings.HasPrefix(t, "#") && !strings.HasPrefix(lines[e], " ")) {
				end = e
				break
			}
		}
		body := strings.TrimRight(strings.Join(lines[start:end], "\n"), "\n \t")
		out = append(out, apmScenario{US: starts[start], Body: body})
	}
	return out
}

// validateApmScenarios is the fail-closed pillar-4 gate: every scenario in
// the .feature must carry exactly one @US tag, and every Scenario Map US must
// have exactly one tagged scenario.
func validateApmScenarios(doc *apmDoc, featureMD string) []string {
	var diffs []string
	byUS := map[string]string{}
	for _, s := range parseApmScenarios(featureMD) {
		title := s.Body
		if idx := strings.IndexByte(title, '\n'); idx >= 0 {
			title = title[:idx]
		}
		if s.US == "" {
			diffs = append(diffs, fmt.Sprintf("scenario %q is missing a @US tag", strings.TrimSpace(title)))
			continue
		}
		if prev, dup := byUS[s.US]; dup {
			diffs = append(diffs, fmt.Sprintf("%s maps to multiple scenarios: %q and %q", s.US, prev, strings.TrimSpace(title)))
			continue
		}
		byUS[s.US] = title
	}
	for _, us := range doc.ScenarioUS {
		if _, ok := byUS[us]; !ok {
			diffs = append(diffs, fmt.Sprintf("Scenario Map %s has no @%s-tagged scenario in the .feature", us, us))
		}
	}
	return diffs
}

// apmFeature / apmGraph are the dry-run JSON shapes and the creation input.
// Phase 5 tasks never become Work Items: their commands live on the epic
// description and are executed by aggregate verification.

type apmFeatureGraph struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	TaskTIDs []string `json:"task_ids"`
}

type apmGraph struct {
	EpicName             string            `json:"epic_name"`
	MilestoneLabel       string            `json:"milestone_label"`
	VerificationCommands []string          `json:"verification_commands"`
	Features             []apmFeatureGraph `json:"features"`
	Tasks                []apmTaskGraph    `json:"tasks"`
	Edges                []apmEdgeGraph    `json:"edges"`
}

type apmTaskGraph struct {
	TID        string   `json:"tid"`
	Feature    string   `json:"feature"`
	Verbatim   string   `json:"description"`
	Acceptance string   `json:"acceptance"`
	US         string   `json:"us,omitempty"`
	DependsOn  []string `json:"depends_on"`
}

type apmEdgeGraph struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// buildApmGraph groups tasks into P-tier features (Phase 1+2+P1→F1,
// P2→F2, P3+polish→F3) and derives edges: Rule A chains the features, Rule B
// sequences tasks inside a feature by the Execution Order block, Rule C gives
// parallel tasks the same predecessor set. Task edges never cross features.
func buildApmGraph(doc *apmDoc, milestone string) *apmGraph {
	graph := &apmGraph{
		EpicName:       doc.Name,
		MilestoneLabel: "milestone:" + milestone,
	}
	featureOf := map[string]string{}
	addTo := func(key, name string, t apmTask) {
		for i := range graph.Features {
			if graph.Features[i].Key == key {
				graph.Features[i].TaskTIDs = append(graph.Features[i].TaskTIDs, t.TID)
				featureOf[t.TID] = key
				return
			}
		}
		graph.Features = append(graph.Features, apmFeatureGraph{Key: key, Name: name, TaskTIDs: []string{t.TID}})
		featureOf[t.TID] = key
	}
	f1Name := doc.TierHeaders["P1"]
	f2Name := doc.TierHeaders["P2"]
	f3Name := doc.TierHeaders["P3"]
	for _, t := range doc.Tasks {
		switch {
		case t.Phase <= 2 || t.Tier == "P1":
			addTo("F1", f1Name, t)
		case t.Tier == "P2":
			addTo("F2", f2Name, t)
		case t.Tier == "P3" || t.Phase == 4:
			addTo("F3", f3Name, t)
		case t.Phase == 5:
			graph.VerificationCommands = append(graph.VerificationCommands, t.Verbatim)
		}
	}
	// Flatten the order block into a global task sequence for predecessor sets.
	var seq []string
	for _, ol := range doc.Order {
		seq = append(seq, ol.Seq...)
	}
	pos := map[string]int{}
	for i, tid := range seq {
		pos[tid] = i
	}
	// Rule C: tasks joined by ║ on one order line are parallel — they share
	// the predecessor set of everything strictly before that line, and must
	// not depend on each other.
	parallelRank := map[string]int{}
	for _, ol := range doc.Order {
		if ol.Parallel {
			for _, tid := range ol.Seq {
				parallelRank[tid] = pos[ol.Seq[0]]
			}
		}
	}
	predecessors := func(tid string) []string {
		tidRank, ok := parallelRank[tid]
		if !ok {
			tidRank = pos[tid]
		}
		var preds []string
		for _, other := range seq {
			rank := pos[other]
			if r, ok := parallelRank[other]; ok {
				rank = r
			}
			if rank < tidRank && featureOf[other] == featureOf[tid] {
				preds = append(preds, other)
			}
		}
		return preds
	}
	for _, t := range doc.Tasks {
		if featureOf[t.TID] == "" {
			continue
		}
		graph.Tasks = append(graph.Tasks, apmTaskGraph{
			TID:        t.TID,
			Feature:    featureOf[t.TID],
			Verbatim:   t.Verbatim,
			Acceptance: t.Acceptance,
			US:         t.US,
			DependsOn:  predecessors(t.TID),
		})
	}
	if len(graph.Features) == 3 {
		graph.Edges = append(graph.Edges, apmEdgeGraph{From: "F2", To: "F1"}, apmEdgeGraph{From: "F3", To: "F2"})
	}
	return graph
}

func writeApmGraphJSON(graph *apmGraph, imported bool, epicID string) {
	payload := map[string]any{
		"epic": map[string]any{
			"name":                  graph.EpicName,
			"milestone_label":       graph.MilestoneLabel,
			"verification_commands": graph.VerificationCommands,
		},
		"features": graph.Features,
		"tasks":    graph.Tasks,
		"edges":    graph.Edges,
		"imported": imported,
	}
	if epicID != "" {
		payload["epic_id"] = epicID
	}
	writeJSON(os.Stdout, payload)
}

func apmImportError(err error, discrepancies []string) error {
	writeJSON(os.Stdout, map[string]any{"error": err.Error(), "discrepancies": discrepancies, "imported": false})
	return errSilentImport
}

var errSilentImport = errors.New("import failed")

// cmdWorkflowImportApm implements `pic workflow import-apm`.
func cmdWorkflowImportApm(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: pic workflow import-apm <tasks.md> --milestone <version> [--dry-run]")
	}
	var flagArgs []string
	dryRun := false
	for _, a := range args[1:] {
		if a == "--dry-run" {
			dryRun = true
			continue
		}
		flagArgs = append(flagArgs, a)
	}
	opts, err := parseOptions(flagArgs)
	if err != nil {
		return err
	}
	milestone := opts["milestone"]
	if milestone == "" {
		return errors.New("import-apm requires --milestone <version>")
	}
	tasksPath := filepath.Clean(args[0])
	content, err := os.ReadFile(tasksPath)
	if err != nil {
		return err
	}
	doc, err := parseApmTasksMD(string(content))
	if err != nil {
		return err
	}
	if doc.Status != "Approved" {
		return apmImportError(fmt.Errorf("task list Status is %q, refusing import: requires Approved", doc.Status), nil)
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if doc.PlanPath != "" {
		if _, err := os.Stat(filepath.Join(root, doc.PlanPath)); err != nil {
			return apmImportError(fmt.Errorf("companion plan %s is missing", doc.PlanPath), nil)
		}
		planBytes, err := os.ReadFile(filepath.Join(root, doc.PlanPath))
		if err != nil {
			return err
		}
		specPath := apmSpecFromPlan(string(planBytes))
		if specPath != "" {
			specBytes, err := os.ReadFile(filepath.Join(root, specPath))
			if err != nil {
				return apmImportError(fmt.Errorf("companion spec %s is unreadable", specPath), nil)
			}
			if !strings.Contains(string(specBytes), "Status: @ready") {
				return apmImportError(fmt.Errorf("companion spec %s is not @ready", specPath), nil)
			}
			doc.Scenarios = map[string]string{}
			for _, s := range parseApmScenarios(string(specBytes)) {
				if s.US != "" {
					doc.Scenarios[s.US] = s.Body
				}
			}
			if diffs := validateApmScenarios(doc, string(specBytes)); len(diffs) > 0 {
				return apmImportError(fmt.Errorf("Scenario Map is inconsistent with the .feature"), diffs)
			}
		}
	}
	if diffs := validateApmTasks(doc); len(diffs) > 0 {
		return apmImportError(fmt.Errorf("Execution Order block is inconsistent with the task list"), diffs)
	}
	contentHash := apmShortHash(content)
	importLabel := "import:" + contentHash

	graph := buildApmGraph(doc, milestone)
	if dryRun {
		writeApmGraphJSON(graph, false, "")
		return nil
	}

	epicID, err := createApmWorkItems(db, doc, graph, importLabel)
	if err != nil {
		if errors.Is(err, errAlreadyImported) {
			return apmImportError(err, nil)
		}
		return err
	}
	writeApmGraphJSON(graph, true, epicID)
	return nil
}

func apmSpecFromPlan(plan string) string {
	for _, line := range strings.Split(plan, "\n") {
		if m := regexp.MustCompile(`^\*\*Spec:\*\* (.+)$`).FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func apmShortHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])[:12]
}

var errAlreadyImported = errors.New("already imported")

// createApmWorkItems writes the whole graph in one transaction; any failure
// rolls back to an empty store.
func createApmWorkItems(db *sql.DB, doc *apmDoc, graph *apmGraph, importLabel string) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var epicID string
	if err := tx.QueryRow(`SELECT work_item_id FROM work_item_labels WHERE label=?`, importLabel).Scan(&epicID); err == nil {
		return "", fmt.Errorf("%w: epic %s already imported this task list", errAlreadyImported, epicID)
	} else if err != sql.ErrNoRows {
		return "", err
	}

	epicID = "wi-" + shortID()
	epicDesc := "Imported from " + doc.PlanPath + "\n\nAggregate verification commands (Phase 5, not Work Items):\n"
	for _, cmd := range graph.VerificationCommands {
		epicDesc += "- " + cmd + "\n"
	}
	if doc.NyquistSection != "" {
		epicDesc += "\n" + doc.NyquistSection + "\n"
	}
	if _, err := tx.Exec(`INSERT INTO work_items(id,type,parent_id,title,description,priority,deferred,planning_depth) VALUES(?,'epic',NULLIF('',''),?,?, 'medium',0,'full')`, epicID, graph.EpicName, epicDesc); err != nil {
		return "", fmt.Errorf("insert epic: %w", err)
	}
	if err := addWorkItemLabels(tx, epicID, []string{graph.MilestoneLabel, importLabel}); err != nil {
		return "", fmt.Errorf("label epic: %w", err)
	}

	featureIDs := map[string]string{}
	featureKeys := []string{"F1", "F2", "F3"}
	titles := map[string]string{}
	for _, f := range graph.Features {
		titles[f.Key] = f.Name
	}
	for i, key := range featureKeys {
		id := "wi-" + shortID()
		featureIDs[key] = id
		if _, err := tx.Exec(`INSERT INTO work_items(id,type,parent_id,title,description,priority,deferred,planning_depth) VALUES(?,'feature',?,?,?, 'medium',0,'full')`, id, epicID, titles[key], "Imported "+titles[key]+" for "+graph.EpicName); err != nil {
			return "", fmt.Errorf("insert feature %s: %w", key, err)
		}
		if i > 0 {
			if _, err := tx.Exec(`INSERT INTO work_item_dependencies(id,work_item_id,depends_on_work_item_id) VALUES(?,?,?)`, "wid-"+shortID(), id, featureIDs[featureKeys[i-1]]); err != nil {
				return "", fmt.Errorf("insert feature edge %s: %w", key, err)
			}
		}
		if err := addWorkItemLabels(tx, id, []string{"apm-feature"}); err != nil {
			return "", fmt.Errorf("label feature %s: %w", key, err)
		}
	}

	taskIDs := map[string]string{}
	for _, t := range graph.Tasks {
		id := "wi-" + shortID()
		taskIDs[t.TID] = id
		desc := t.Verbatim + "\n\nAcceptance criteria:\n" + t.Acceptance
		for _, us := range strings.Fields(t.US) {
			if body, ok := doc.Scenarios[us]; ok {
				desc += "\n\nBehavior context (" + us + "):\n" + body
			}
		}
		if _, err := tx.Exec(`INSERT INTO work_items(id,type,parent_id,title,description,priority,deferred,planning_depth) VALUES(?,'task',?,?,?,'medium',0,'full')`, id, featureIDs[t.Feature], taskTitle(t.Verbatim), desc); err != nil {
			return "", fmt.Errorf("insert task %s: %w", t.TID, err)
		}
		if err := addWorkItemLabels(tx, id, []string{"apm-task"}); err != nil {
			return "", fmt.Errorf("label task %s: %w", t.TID, err)
		}
	}
	for _, t := range graph.Tasks {
		for _, dep := range t.DependsOn {
			if _, err := tx.Exec(`INSERT INTO work_item_dependencies(id,work_item_id,depends_on_work_item_id) VALUES(?,?,?)`, "wid-"+shortID(), taskIDs[t.TID], taskIDs[dep]); err != nil {
				return "", fmt.Errorf("insert task edge %s->%s: %w", t.TID, dep, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return epicID, nil
}

// taskTitle extracts the human title from the verbatim block's first line,
// stripping the [TID] and tag brackets.
func taskTitle(verbatim string) string {
	line := strings.TrimSpace(strings.SplitN(verbatim, "\n", 2)[0])
	line = regexp.MustCompile(`^- \[T\d+\]\s*`).ReplaceAllString(line, "")
	for strings.HasPrefix(line, "[") {
		end := strings.Index(line, "]")
		if end < 0 {
			break
		}
		line = strings.TrimSpace(line[end+1:])
	}
	return line
}
