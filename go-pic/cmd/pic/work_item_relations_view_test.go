package main

// Tests for the work-item show graph surface (RLB-GAP-004): children and
// dependency-edge enumeration must come from the sanctioned CLI so the
// contractor never needs raw sqlite for subtree/edge facts.

import (
	"testing"
)

func TestWorkItemShowIncludesChildrenAndDependencyEdges(t *testing.T) {
	t.Parallel()
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Graph parent"))
	taskA := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Child A", "--parent", epic["id"].(string)))
	taskB := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Child B", "--parent", epic["id"].(string)))
	dbPath := root + "/.pi/tasks.db"
	runSQLite(t, dbPath, `INSERT INTO work_item_dependencies(id,work_item_id,depends_on_work_item_id) VALUES('wid-test-1','`+taskA["id"].(string)+`','`+taskB["id"].(string)+`')`)

	shownEpic := asObject(t, runPic(t, bin, root, home, "work-item", "show", epic["id"].(string)))
	children, ok := shownEpic["children"].([]any)
	if !ok || len(children) != 2 {
		t.Fatalf("epic show must list its children: %#v", shownEpic["children"])
	}
	childIDs := map[string]bool{}
	for _, c := range children {
		childIDs[c.(map[string]any)["id"].(string)] = true
	}
	if !childIDs[taskA["id"].(string)] || !childIDs[taskB["id"].(string)] {
		t.Fatalf("children ids = %#v", children)
	}
	if _, ok := shownEpic["dependency_edges"]; !ok {
		t.Fatalf("show must always carry dependency_edges key: %#v", shownEpic)
	}

	shownA := asObject(t, runPic(t, bin, root, home, "work-item", "show", taskA["id"].(string)))
	edgesA := shownA["dependency_edges"].([]any)
	if len(edgesA) != 1 || edgesA[0].(map[string]any)["depends_on_work_item_id"] != taskB["id"] {
		t.Fatalf("outgoing dependency edge missing: %#v", edgesA)
	}

	shownB := asObject(t, runPic(t, bin, root, home, "work-item", "show", taskB["id"].(string)))
	edgesB := shownB["dependency_edges"].([]any)
	if len(edgesB) != 1 || edgesB[0].(map[string]any)["work_item_id"] != taskA["id"] {
		t.Fatalf("incoming dependency edge missing: %#v", edgesB)
	}

	leaf := asObject(t, runPic(t, bin, root, home, "work-item", "show", taskA["id"].(string)))
	if kids, ok := leaf["children"].([]any); !ok || len(kids) != 0 {
		t.Fatalf("childless item must report empty children: %#v", leaf["children"])
	}
}
