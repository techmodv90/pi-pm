package apm

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	HeaderRe      = regexp.MustCompile(`^# Tasks: (.+)$`)
	FieldRe       = regexp.MustCompile(`^\*\*(Feature|Plan|Status):\*\* (.+)$`)
	PhaseRe       = regexp.MustCompile(`^## Phase (\d+):`)
	TierRe        = regexp.MustCompile(`^### (P[123]\+?):\s*(.+)$`)
	TaskRe        = regexp.MustCompile(`^- \[(T\d{3})\]\s*(.*)$`)
	DetailRe      = regexp.MustCompile(`^  - (Files|Trace|Acceptance): (.*)$`)
	ScenarioUSRe  = regexp.MustCompile(`^\| (US\d+) \|`)
	OrderLineRe   = regexp.MustCompile(`^Phase (\d+)(?:\s+(P[123]\+?))?:\s*(.+)$`)
	TaskTagUSRe   = regexp.MustCompile(`\[US(\d+)\]`)
	TaskTagTierRe = regexp.MustCompile(`\[(P[123])\]`)
)

// Doc and Graph are the dry-run JSON shapes and the creation input. Phase 5
// tasks never become Work Items: their commands live on the epic description
// and are executed by aggregate verification.

var errSilentImport = errors.New("import failed")

var errAlreadyImported = errors.New("already imported")

type Task struct {
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

type Doc struct {
	Name        string
	Status      string
	PlanPath    string
	ScenarioUS  []string
	TierHeaders map[string]string // "P1" -> "P1: Critical Path" (verbatim header text)
	Tasks       []Task
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

// ParseTasksMD converts .tasks.md text into the import document. It never
// infers: whatever is not in the file is absent from the doc.
func ParseTasksMD(content string) (*Doc, error) {
	doc := &Doc{TierHeaders: map[string]string{}}
	lines := strings.Split(content, "\n")
	phase := 0
	tier := ""
	inOrder := false
	var task *Task
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
		if m := HeaderRe.FindStringSubmatch(trimmed); m != nil && doc.Name == "" {
			doc.Name = m[1]
			continue
		}
		if m := FieldRe.FindStringSubmatch(trimmed); m != nil {
			switch m[1] {
			case "Plan":
				doc.PlanPath = strings.TrimSpace(m[2])
			case "Status":
				doc.Status = strings.TrimSpace(m[2])
			}
			continue
		}
		if m := PhaseRe.FindStringSubmatch(trimmed); m != nil {
			flush()
			fmt.Sscanf(m[1], "%d", &phase)
			tier = ""
			inOrder = false
			continue
		}
		if m := TierRe.FindStringSubmatch(trimmed); m != nil {
			tier = strings.TrimSuffix(m[1], "+")
			doc.TierHeaders[tier] = strings.TrimSpace(m[1] + ": " + m[2])
			continue
		}
		if m := ScenarioUSRe.FindStringSubmatch(trimmed); m != nil {
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
			if m := OrderLineRe.FindStringSubmatch(trimmed); m != nil {
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
		if m := TaskRe.FindStringSubmatch(line); m != nil {
			flush()
			nt := Task{TID: m[1], Phase: phase, Tier: tier, Title: strings.TrimSpace(m[2]), Verbatim: line}
			if strings.Contains(m[2], "[P]") {
				nt.Parallel = true
			}
			for _, us := range TaskTagUSRe.FindAllStringSubmatch(m[2], -1) {
				nt.US = strings.TrimSpace(nt.US + " US" + us[1])
			}
			if mt := TaskTagTierRe.FindStringSubmatch(m[2]); mt != nil {
				nt.Tier = mt[1]
			}
			task = &nt
			continue
		}
		if task != nil {
			if m := DetailRe.FindStringSubmatch(line); m != nil {
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

// ValidateTasks implements Rule D: the Execution Order block must agree
// with the task list exactly — every mismatch is reported, none is repaired.
func ValidateTasks(doc *Doc) []string {
	var diffs []string
	byID := map[string]Task{}
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

func scenarioUSPresent(doc *Doc, us string) bool {
	for _, known := range doc.ScenarioUS {
		if known == us {
			return true
		}
	}
	return false
}

type Scenario struct {
	US   string
	Body string
}

// ParseScenarios extracts verbatim Gherkin scenario blocks from a .feature
// keyed by the @US<n> tag directly above the Scenario: line (authoring rule:
// every Scenario carries exactly one @US tag).
func ParseScenarios(featureMD string) []Scenario {
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
	var out []Scenario
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
		out = append(out, Scenario{US: starts[start], Body: body})
	}
	return out
}

// ValidateScenarios is the fail-closed pillar-4 gate: every scenario in
// the .feature must carry exactly one @US tag, and every Scenario Map US must
// have exactly one tagged scenario.
func ValidateScenarios(doc *Doc, featureMD string) []string {
	var diffs []string
	byUS := map[string]string{}
	for _, s := range ParseScenarios(featureMD) {
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
