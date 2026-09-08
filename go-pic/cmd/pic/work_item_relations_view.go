package main

// Work-item show graph surface (RLB-GAP-004): the sanctioned CLI must expose
// subtree and dependency-edge facts so the contractor never falls back to raw
// sqlite reads for enumeration. Split from the oversized work_items.go
// monolith per the sub-400 LoC policy.

import (
	"database/sql"

	"github.com/earendil-works/task-system/go-pic/internal/store"
)

// attachWorkItemGraph enriches a work-item row with its direct children and
// every dependency edge (outgoing and incoming) touching it.
func attachWorkItemGraph(db *sql.DB, item map[string]any) error {
	id, _ := item["id"].(string)
	children, err := store.QueryMaps(db, `SELECT id,type,status,title FROM work_items WHERE parent_id=? ORDER BY created_at,id`, id)
	if err != nil {
		return err
	}
	if children == nil {
		children = []map[string]any{}
	}
	item["children"] = children

	edges, err := store.QueryMaps(db, `SELECT work_item_id,depends_on_work_item_id FROM work_item_dependencies WHERE work_item_id=? OR depends_on_work_item_id=? ORDER BY created_at,id`, id, id)
	if err != nil {
		return err
	}
	if edges == nil {
		edges = []map[string]any{}
	}
	item["dependency_edges"] = edges
	return nil
}
