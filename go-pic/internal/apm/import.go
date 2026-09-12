package apm

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/earendil-works/task-system/go-pic/internal/store"
	workitem "github.com/earendil-works/task-system/go-pic/internal/work-item"
)

func ImportError(err error, discrepancies []string) error {
	store.WriteJSON(os.Stdout, map[string]any{"error": err.Error(), "discrepancies": discrepancies, "imported": false})
	return errSilentImport
}

// Import implements `pic workflow import-apm`.
func Import(db *sql.DB, args []string) error {
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
	opts, err := store.ParseOptions(flagArgs)
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
	doc, err := ParseTasksMD(string(content))
	if err != nil {
		return err
	}
	if doc.Status != "Approved" {
		return ImportError(fmt.Errorf("task list Status is %q, refusing import: requires Approved", doc.Status), nil)
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if doc.PlanPath != "" {
		if _, err := os.Stat(filepath.Join(root, doc.PlanPath)); err != nil {
			return ImportError(fmt.Errorf("companion plan %s is missing", doc.PlanPath), nil)
		}
		planBytes, err := os.ReadFile(filepath.Join(root, doc.PlanPath))
		if err != nil {
			return err
		}
		specPath := specFromPlan(string(planBytes))
		if specPath != "" {
			specBytes, err := os.ReadFile(filepath.Join(root, specPath))
			if err != nil {
				return ImportError(fmt.Errorf("companion spec %s is unreadable", specPath), nil)
			}
			if !strings.Contains(string(specBytes), "Status: @ready") {
				return ImportError(fmt.Errorf("companion spec %s is not @ready", specPath), nil)
			}
			doc.Scenarios = map[string]string{}
			for _, s := range ParseScenarios(string(specBytes)) {
				if s.US != "" {
					doc.Scenarios[s.US] = s.Body
				}
			}
			if diffs := ValidateScenarios(doc, string(specBytes)); len(diffs) > 0 {
				return ImportError(fmt.Errorf("Scenario Map is inconsistent with the .feature"), diffs)
			}
		}
	}
	if diffs := ValidateTasks(doc); len(diffs) > 0 {
		return ImportError(fmt.Errorf("Execution Order block is inconsistent with the task list"), diffs)
	}
	contentHash := ShortHash(content)
	importLabel := "import:" + contentHash

	graph := BuildGraph(doc, milestone)
	if dryRun {
		writeGraphJSON(graph, false, "")
		return nil
	}

	epicID, err := createWorkItems(db, doc, graph, importLabel)
	if err != nil {
		if errors.Is(err, errAlreadyImported) {
			return ImportError(err, nil)
		}
		return err
	}
	writeGraphJSON(graph, true, epicID)
	return nil
}

func specFromPlan(plan string) string {
	for _, line := range strings.Split(plan, "\n") {
		if m := regexp.MustCompile(`^\*\*Spec:\*\* (.+)$`).FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func ShortHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])[:12]
}

// createWorkItems writes the whole graph in one transaction; any failure
// rolls back to an empty store.
func createWorkItems(db *sql.DB, doc *Doc, graph *Graph, importLabel string) (string, error) {
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

	epicID = "wi-" + store.ShortID()
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
	if err := workitem.AddLabels(tx, epicID, []string{graph.MilestoneLabel, importLabel}); err != nil {
		return "", fmt.Errorf("label epic: %w", err)
	}

	featureIDs := map[string]string{}
	featureKeys := []string{"F1", "F2", "F3"}
	titles := map[string]string{}
	for _, f := range graph.Features {
		titles[f.Key] = f.Name
	}
	for i, key := range featureKeys {
		id := "wi-" + store.ShortID()
		featureIDs[key] = id
		if _, err := tx.Exec(`INSERT INTO work_items(id,type,parent_id,title,description,priority,deferred,planning_depth) VALUES(?,'feature',?,?,?, 'medium',0,'full')`, id, epicID, titles[key], "Imported "+titles[key]+" for "+graph.EpicName); err != nil {
			return "", fmt.Errorf("insert feature %s: %w", key, err)
		}
		if i > 0 {
			if _, err := tx.Exec(`INSERT INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id) VALUES(?,?,'blocks',?)`, "wir-"+store.ShortID(), id, featureIDs[featureKeys[i-1]]); err != nil {
				return "", fmt.Errorf("insert feature edge %s: %w", key, err)
			}
		}
		if err := workitem.AddLabels(tx, id, []string{"apm-feature"}); err != nil {
			return "", fmt.Errorf("label feature %s: %w", key, err)
		}
	}

	taskIDs := map[string]string{}
	for _, t := range graph.Tasks {
		id := "wi-" + store.ShortID()
		taskIDs[t.TID] = id
		desc := t.Verbatim + "\n\nAcceptance criteria:\n" + t.Acceptance
		for _, us := range strings.Fields(t.US) {
			if body, ok := doc.Scenarios[us]; ok {
				desc += "\n\nBehavior context (" + us + "):\n" + body
			}
		}
		if _, err := tx.Exec(`INSERT INTO work_items(id,type,parent_id,title,description,priority,deferred,planning_depth) VALUES(?,'task',?,?,?,'medium',0,'full')`, id, featureIDs[t.Feature], TaskTitle(t.Verbatim), desc); err != nil {
			return "", fmt.Errorf("insert task %s: %w", t.TID, err)
		}
		if err := workitem.AddLabels(tx, id, []string{"apm-task"}); err != nil {
			return "", fmt.Errorf("label task %s: %w", t.TID, err)
		}
	}
	for _, t := range graph.Tasks {
		for _, dep := range t.DependsOn {
			if _, err := tx.Exec(`INSERT INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id) VALUES(?,?,'blocks',?)`, "wir-"+store.ShortID(), taskIDs[t.TID], taskIDs[dep]); err != nil {
				return "", fmt.Errorf("insert task edge %s->%s: %w", t.TID, dep, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return epicID, nil
}

// TaskTitle extracts the human title from the verbatim block's first line,
// stripping the [TID] and tag brackets.
func TaskTitle(verbatim string) string {
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
