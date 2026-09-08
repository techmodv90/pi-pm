package activity

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/earendil-works/task-system/go-pic/internal/project"
	"strings"

	"github.com/earendil-works/task-system/go-pic/internal/store"
)

func Activity(args []string) error {
	if len(args) == 0 {
		return errors.New("activity subcommand required")
	}
	db, err := project.OpenDB()
	if err != nil {
		return err
	}
	defer db.Close()
	switch args[0] {
	case "update":
		opts, err := store.ParseOptions(args[1:])
		if err != nil {
			return err
		}
		session := opts["session"]
		if session == "" {
			return errors.New("--session is required")
		}
		_, err = db.Exec(`INSERT INTO session_activity (session_id, task_id, status, current_step_label, last_skill, updated_at) VALUES (?, ?, ?, ?, ?, datetime('now')) ON CONFLICT(session_id) DO UPDATE SET task_id = COALESCE(NULLIF(excluded.task_id, ''), task_id), status = excluded.status, current_step_label = COALESCE(NULLIF(excluded.current_step_label, ''), current_step_label), last_skill = COALESCE(NULLIF(excluded.last_skill, ''), last_skill), updated_at = datetime('now')`, session, opts["task"], project.FirstNonEmpty(opts["status"], "active"), opts["step"], opts["skill"])
		if err != nil {
			return err
		}
		store.WriteJSON(os.Stdout, map[string]any{"ok": true})
		return nil
	case "list":
		rows, err := store.QueryMaps(db, `SELECT sa.session_id,sa.task_id,COALESCE(wi.title,'') as task_title,sa.status,COALESCE(children.done,0) as done,COALESCE(children.total,0) as total,sa.last_skill,sa.updated_at FROM session_activity sa LEFT JOIN work_items wi ON wi.id=sa.task_id AND sa.task_id!='' LEFT JOIN (SELECT parent_id,SUM(CASE WHEN status='done' THEN 1 ELSE 0 END) as done,COUNT(*) as total FROM work_items WHERE parent_id IS NOT NULL GROUP BY parent_id) children ON children.parent_id=sa.task_id WHERE sa.status='active' AND sa.task_id!='' AND datetime(sa.updated_at)>datetime('now','-30 seconds') ORDER BY datetime(sa.updated_at) DESC`)
		if err != nil {
			return err
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		store.WriteJSON(os.Stdout, rows)
		return nil
	default:
		return fmt.Errorf("unknown activity subcommand: %s", args[0])
	}
}

func Search(args []string) error {
	if len(args) < 1 {
		return errors.New("search requires query")
	}
	db, err := project.OpenDB()
	if err != nil {
		return err
	}
	defer db.Close()
	q := "%" + strings.ToLower(args[0]) + "%"
	results, _ := store.QueryMaps(db, `SELECT type,id,title,status,priority,parent_id FROM work_items WHERE lower(title) LIKE ? OR lower(description) LIKE ? ORDER BY created_at,id`, q, q)
	store.WriteJSON(os.Stdout, results)
	return nil
}

func Markdown(args []string) error {
	db, err := project.OpenDB()
	if err != nil {
		return err
	}
	defer db.Close()
	targetType, id, query := "list", "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--work-item":
			i++
			if i >= len(args) {
				return errors.New("--work-item requires a value")
			}
			targetType, id = "work-item", args[i]
		case "--search":
			i++
			if i >= len(args) {
				return errors.New("--search requires a value")
			}
			targetType, query = "search", args[i]
		default:
			return fmt.Errorf("unknown markdown option: %s", args[i])
		}
	}
	text, err := MarkdownText(db, targetType, id, query)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

func MarkdownText(db *sql.DB, targetType, id, query string) (string, error) {
	switch targetType {
	case "work-item":
		item, err := store.QueryOne(db, `SELECT * FROM work_items WHERE id=?`, id)
		if err != nil {
			return fmt.Sprintf("# Error: Work Item %s not found", id), nil
		}
		children, _ := store.QueryMaps(db, `SELECT * FROM work_items WHERE parent_id=? ORDER BY created_at,id`, id)
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\nType: %s\nStatus: %s\n\n", item["title"], item["type"], item["status"])
		for _, child := range children {
			fmt.Fprintf(&b, "- %s (%s)\n", child["title"], child["status"])
		}
		return b.String(), nil
	case "search":
		return fmt.Sprintf("Search results for %q:\n", query), nil
	default:
		items, _ := store.QueryMaps(db, `SELECT * FROM work_items ORDER BY created_at DESC`)
		var b strings.Builder
		b.WriteString("# Work Items\n\n")
		for _, item := range items {
			fmt.Fprintf(&b, "- %s (%s)\n", item["title"], item["status"])
		}
		return b.String(), nil
	}
}
