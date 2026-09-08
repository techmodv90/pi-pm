package workitem

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const Columns = `id,type,parent_id,title,description,status,priority,deferred,claimed_at,claimed_by,review_status,review_notes,planning_depth,created_at,decomposition_mode,decomposition_reason,paired_contract_node,source_graph_artifact_id,source_graph_revision,source_graph_content_hash`

var labelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]{0,254}$`)

// Contract deviation approval: preserve planning and renew only execution authority.
func SetStatus(db *sql.DB, id, status string) (map[string]any, error) {
	if status == "done" {
		var managed int
		if err := db.QueryRow(`SELECT COUNT(*) FROM work_items wi JOIN work_item_instruction_packs p ON p.work_item_id=wi.id AND p.status='active' WHERE wi.id=? AND wi.type IN ('task','bug','chore')`, id).Scan(&managed); err != nil {
			return nil, err
		}
		if managed > 0 {
			if err := validateExecutableClosure(db, id); err != nil {
				return nil, err
			}
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRow(`SELECT status FROM work_items WHERE id=?`, id).Scan(&current); err != nil {
		return nil, fmt.Errorf("Work Item %s not found", id)
	}
	if (current == "done" || current == "cancelled") && status != current {
		return nil, errors.New("completed or cancelled managed work requires a new TIP generation before reopening")
	}
	if status == "in_progress" {
		var managed, active int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, id).Scan(&managed)
		_ = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND stage IN ('worker','autofix') AND status IN ('claimed','running')`, id).Scan(&active)
		if managed > 0 && active != 1 {
			return nil, errors.New("managed Work Item requires one active mutation claim before entering in_progress")
		}
	}
	if status == "cancelled" {
		if _, err = tx.Exec(`UPDATE pipeline_runs SET status='cancelled',error='Work Item cancelled',updated_at=datetime('now'),completed_at=datetime('now') WHERE task_id=? AND status IN ('claimed','running')`, id); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`WITH RECURSIVE descendants(id) AS (SELECT id FROM work_items WHERE parent_id=? UNION ALL SELECT wi.id FROM work_items wi JOIN descendants d ON wi.parent_id=d.id) UPDATE pipeline_runs SET status='cancelled',error='Parent Work Item cancelled',updated_at=datetime('now'),completed_at=datetime('now') WHERE task_id IN (SELECT id FROM descendants) AND status IN ('claimed','running')`, id); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`WITH RECURSIVE descendants(id) AS (SELECT id FROM work_items WHERE parent_id=? UNION ALL SELECT wi.id FROM work_items wi JOIN descendants d ON wi.parent_id=d.id) UPDATE work_items SET status='cancelled',claimed_at='',claimed_by='' WHERE id IN (SELECT id FROM descendants) AND status NOT IN ('done','cancelled')`, id); err != nil {
			return nil, err
		}
	}
	result, err := tx.Exec(`UPDATE work_items SET status=?,claimed_at=CASE WHEN ? IN ('open','cancelled') THEN '' ELSE claimed_at END,claimed_by=CASE WHEN ? IN ('open','cancelled') THEN '' ELSE claimed_by END WHERE id=?`, status, status, status, id)
	if err != nil {
		return nil, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return nil, fmt.Errorf("Work Item %s not found", id)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return ByID(db, id)
}
func parseLabels(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	labels := []string{}
	for _, label := range strings.Split(value, ",") {
		if !labelPattern.MatchString(label) {
			return nil, fmt.Errorf("invalid label: %s", label)
		}
		if !seen[label] {
			seen[label] = true
			labels = append(labels, label)
		}
	}
	return labels, nil
}
func AddLabels(tx *sql.Tx, id string, labels []string) error {
	for _, label := range labels {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO work_item_labels(work_item_id,label) VALUES(?,?)`, id, label); err != nil {
			return err
		}
	}
	return nil
}
func Labels(db *sql.DB, id string) ([]string, error) {
	rows, err := db.Query(`SELECT label FROM work_item_labels WHERE work_item_id=? ORDER BY label`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := []string{}
	for rows.Next() {
		var label string
		if err := rows.Scan(&label); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}
func AttachLabels(db *sql.DB, items []map[string]any) error {
	for _, item := range items {
		labels, err := Labels(db, item["id"].(string))
		if err != nil {
			return err
		}
		item["labels"] = labels
	}
	return nil
}
func Label(db *sql.DB, args []string) error {
	if len(args) == 1 && args[0] == "list-all" {
		rows, err := queryMaps(db, `SELECT label,COUNT(*) AS count FROM work_item_labels GROUP BY label ORDER BY label`)
		if err == nil {
			writeJSON(os.Stdout, rows)
		}
		return err
	}
	if len(args) < 2 || !contains([]string{"add", "remove", "list"}, args[0]) {
		return errors.New("usage: pic work-item label <add|remove|list> <id> [labels] | label list-all")
	}
	if _, err := ByID(db, args[1]); err != nil {
		return err
	}
	if args[0] == "list" {
		labels, err := Labels(db, args[1])
		if err == nil {
			writeJSON(os.Stdout, labels)
		}
		return err
	}
	if len(args) != 3 {
		return fmt.Errorf("usage: pic work-item label %s <id> <a,b>", args[0])
	}
	labels, err := parseLabels(args[2])
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if args[0] == "add" {
		err = AddLabels(tx, args[1], labels)
	} else {
		for _, label := range labels {
			if _, err = tx.Exec(`DELETE FROM work_item_labels WHERE work_item_id=? AND label=?`, args[1], label); err != nil {
				return err
			}
		}
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item, err := ByID(db, args[1])
	if err == nil {
		writeJSON(os.Stdout, item)
	}
	return err
}
func List(db *sql.DB, args []string) ([]map[string]any, error) {
	opts, err := parseOptions(args)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + Columns + ` FROM work_items wi WHERE 1=1`
	values := []any{}
	for _, filter := range []struct {
		name string
		all  bool
	}{{"label", true}, {"label-any", false}} {
		labels, err := parseLabels(opts[filter.name])
		if err != nil {
			return nil, err
		}
		if len(labels) == 0 {
			continue
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(labels)), ",")
		query += ` AND (SELECT COUNT(DISTINCT label) FROM work_item_labels WHERE work_item_id=wi.id AND label IN (` + placeholders + `)) `
		for _, label := range labels {
			values = append(values, label)
		}
		if filter.all {
			query += `= ?`
			values = append(values, len(labels))
		} else {
			query += `> 0`
		}
	}
	rows, err := queryMaps(db, query+` ORDER BY created_at,id`, values...)
	if err != nil {
		return nil, err
	}
	if err = AttachLabels(db, rows); err != nil {
		return nil, err
	}
	return rows, nil
}
func Create(db *sql.DB, args []string) error {
	if len(args) < 2 || !contains([]string{"epic", "feature", "task", "bug", "chore", "gate"}, args[0]) {
		return errors.New("usage: pic work-item create <epic|feature|task|bug|chore|gate> <title> [--parent <id>] [--description <text>] [--priority <level>] [--labels <a,b>] [--planning-depth <level>]")
	}
	opts, err := parseOptions(args[2:])
	if err != nil {
		return err
	}
	priority := firstNonEmpty(opts["priority"], "medium")
	if !contains([]string{"low", "medium", "high"}, priority) {
		return fmt.Errorf("invalid priority: %s", priority)
	}
	planningDepth := firstNonEmpty(opts["planning-depth"], "full")
	if !ValidPlanningDepth(planningDepth) {
		return fmt.Errorf("invalid planning depth %s: must be quick|standard|designed|full", planningDepth)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	parent := opts["parent"]
	labels, err := parseLabels(opts["labels"])
	if err != nil {
		return err
	}
	if err = validateWorkItemParent(tx, "", parent); err != nil {
		return err
	}
	id := "wi-" + shortID()
	deferred := 0
	if opts["deferred"] == "1" || opts["deferred"] == "true" {
		deferred = 1
	}
	if _, err = tx.Exec(`INSERT INTO work_items(id,type,parent_id,title,description,priority,deferred,planning_depth) VALUES(?,?,NULLIF(?,''),?,?,?,?,?)`, id, args[0], parent, args[1], opts["description"], priority, deferred, planningDepth); err != nil {
		return err
	}
	if parent != "" {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO work_item_labels(work_item_id,label) SELECT ?,label FROM work_item_labels WHERE work_item_id=?`, id, parent); err != nil {
			return err
		}
	}
	if err = AddLabels(tx, id, labels); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item, err := ByID(db, id)
	if err == nil {
		writeJSON(os.Stdout, item)
	}
	return err
}
func Update(db *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: pic work-item update <id> [--title <text>] [--description <text>] [--parent <id>]")
	}
	opts, err := parseOptions(args[1:])
	if err != nil {
		return err
	}
	if len(opts) == 0 {
		return errors.New("no fields to update")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = byIDTx(tx, args[0]); err != nil {
		return err
	}
	if parent, ok := opts["parent"]; ok {
		if err = validateWorkItemParent(tx, args[0], parent); err != nil {
			return err
		}
	}
	sets, values := []string{}, []any{}
	for _, field := range []string{"title", "description", "priority"} {
		if value, ok := opts[field]; ok {
			sets = append(sets, field+"=?")
			values = append(values, value)
		}
	}
	if parent, ok := opts["parent"]; ok {
		sets = append(sets, "parent_id=NULLIF(?,'')")
		values = append(values, parent)
	}
	if len(sets) == 0 {
		return errors.New("no supported fields to update")
	}
	values = append(values, args[0])
	if _, err = tx.Exec(`UPDATE work_items SET `+strings.Join(sets, ",")+` WHERE id=?`, values...); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	item, err := ByID(db, args[0])
	if err == nil {
		writeJSON(os.Stdout, item)
	}
	return err
}
func validateWorkItemParent(tx *sql.Tx, id, parentID string) error {
	if parentID == "" {
		return nil
	}
	var kind string
	if err := tx.QueryRow(`SELECT type FROM work_items WHERE id=?`, parentID).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("Parent Work Item %s not found", parentID)
		}
		return err
	}
	if kind != "epic" && kind != "feature" {
		return fmt.Errorf("%s Work Items cannot contain children", kind)
	}
	if id == "" {
		return nil
	}
	var cycle int
	err := tx.QueryRow(`WITH RECURSIVE ancestors(id) AS (
		SELECT ? UNION ALL SELECT parent_id FROM work_items JOIN ancestors ON work_items.id=ancestors.id WHERE parent_id IS NOT NULL
	) SELECT EXISTS(SELECT 1 FROM ancestors WHERE id=?)`, parentID, id).Scan(&cycle)
	if err != nil {
		return err
	}
	if cycle != 0 {
		return errors.New("containment cycle")
	}
	return nil
}
func ByID(db *sql.DB, id string) (map[string]any, error) {
	rows, err := queryMaps(db, `SELECT `+Columns+` FROM work_items WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("Work Item %s not found", id)
	}
	if err := AttachLabels(db, rows); err != nil {
		return nil, err
	}
	return rows[0], nil
}
func byIDTx(tx *sql.Tx, id string) (map[string]any, error) {
	rows, err := queryMaps(tx, `SELECT `+Columns+` FROM work_items WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("Work Item %s not found", id)
	}
	return rows[0], nil
}
