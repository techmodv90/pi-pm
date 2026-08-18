package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// setupCorrectiveAggregate builds a fresh project with an epic aggregate and one
// completed child task, returning the binary, project root/home, and the IDs.
func setupCorrectiveAggregate(t *testing.T) (bin, root, home, epicID, childID string) {
	t.Helper()
	bin = buildPic(t)
	root, home = initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Corrective aggregate"))
	epicID = epic["id"].(string)
	child := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Completed child", "--parent", epicID))
	childID = child["id"].(string)
	runPic(t, bin, root, home, "work-item", "status", childID, "done")
	return bin, root, home, epicID, childID
}

func openCorrectiveDB(t *testing.T, root string) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(root, ".pi", "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestCorrectivePassed: a passed aggregate verification creates no corrective
// Bug, no link, no relation, and no corrective event.
func TestCorrectivePassed(t *testing.T) {
	bin, root, home, epicID, childID := setupCorrectiveAggregate(t)
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "passed", "delivery verified", "--actor-role", "contractor"))
	if report["status"] != "passed" || report["corrective_bug_id"] != "" {
		t.Fatalf("passed aggregate report = %#v", report)
	}
	db := openCorrectiveDB(t, root)
	var bugs, links, relations, events int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=? AND type='bug'`, epicID).Scan(&bugs)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_corrective_bugs WHERE verification_report_id=?`, report["id"]).Scan(&links)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_relations WHERE work_item_id=? AND relation_type='related'`, epicID).Scan(&relations)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type LIKE 'corrective%'`, epicID).Scan(&events)
	if bugs != 0 || links != 0 || relations != 0 || events != 0 {
		t.Fatalf("passed verification created corrective state: bugs=%d links=%d relations=%d events=%d", bugs, links, relations, events)
	}
	var childStatus string
	_ = db.QueryRow(`SELECT status FROM work_items WHERE id=?`, childID).Scan(&childStatus)
	if childStatus != "done" {
		t.Fatalf("passed verification mutilated completed child: status=%q", childStatus)
	}
}

// TestCorrectiveFailed: a failed report links a single corrective Bug with an
// owner-decision-pending policy and waits for explicit owner approval.
func TestCorrectiveFailed(t *testing.T) {
	bin, root, home, epicID, _ := setupCorrectiveAggregate(t)
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "failed", "release check failed", "--actor-role", "contractor"))
	bugID, _ := report["corrective_bug_id"].(string)
	if report["status"] != "failed" || bugID == "" {
		t.Fatalf("failed aggregate report = %#v", report)
	}
	db := openCorrectiveDB(t, root)
	var approvalRequired int
	if err := db.QueryRow(`SELECT owner_approval_required FROM work_item_corrective_bugs WHERE verification_report_id=?`, report["id"]).Scan(&approvalRequired); err != nil {
		t.Fatalf("missing corrective link for failed report: %v", err)
	}
	if approvalRequired != 1 {
		t.Fatalf("failed corrective Bug owner_approval_required = %d, want 1", approvalRequired)
	}
	var requirements int
	_ = db.QueryRow(`SELECT COUNT(*) FROM requirements WHERE task_id=?`, bugID).Scan(&requirements)
	if requirements != 1 {
		t.Fatalf("failed corrective Bug requirements = %d, want 1", requirements)
	}
	var pending int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='corrective_owner_decision_pending'`, epicID).Scan(&pending)
	if pending != 1 {
		t.Fatalf("failed report owner-decision-pending events = %d, want 1", pending)
	}
}

// TestCorrectivePartial and TestCorrectiveBlocked: partial and blocked outcomes
// link a single corrective Bug with automatic scheduling and an owner
// notification.
func TestCorrectivePartial(t *testing.T) {
	bin, root, home, epicID, status := setupCorrectiveAggregate(t)
	_ = status
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "partial", "partial release", "--actor-role", "contractor"))
	bugID, _ := report["corrective_bug_id"].(string)
	if report["status"] != "partial" || bugID == "" {
		t.Fatalf("partial aggregate report = %#v", report)
	}
	db := openCorrectiveDB(t, root)
	var approvalRequired int
	if err := db.QueryRow(`SELECT owner_approval_required FROM work_item_corrective_bugs WHERE verification_report_id=?`, report["id"]).Scan(&approvalRequired); err != nil {
		t.Fatalf("missing corrective link for partial report: %v", err)
	}
	if approvalRequired != 0 {
		t.Fatalf("partial corrective Bug owner_approval_required = %d, want 0", approvalRequired)
	}
	var notified int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='corrective_scheduled'`, epicID).Scan(&notified)
	if notified != 1 {
		t.Fatalf("partial report owner-notification events = %d, want 1", notified)
	}
}

func TestCorrectiveBlocked(t *testing.T) {
	bin, root, home, epicID, _ := setupCorrectiveAggregate(t)
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "blocked", "environment blocked release", "--actor-role", "contractor"))
	bugID, _ := report["corrective_bug_id"].(string)
	if report["status"] != "blocked" || bugID == "" {
		t.Fatalf("blocked aggregate report = %#v", report)
	}
	db := openCorrectiveDB(t, root)
	var approvalRequired int
	if err := db.QueryRow(`SELECT owner_approval_required FROM work_item_corrective_bugs WHERE verification_report_id=?`, report["id"]).Scan(&approvalRequired); err != nil {
		t.Fatalf("missing corrective link for blocked report: %v", err)
	}
	if approvalRequired != 0 {
		t.Fatalf("blocked corrective Bug owner_approval_required = %d, want 0", approvalRequired)
	}
	var notified int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type='corrective_scheduled'`, epicID).Scan(&notified)
	if notified != 1 {
		t.Fatalf("blocked report owner-notification events = %d, want 1", notified)
	}
}

// TestCorrectiveExactlyOnce: each non-passing report links to exactly one
// corrective Bug; duplicate linkage for the same report is rejected; and a new
// report produces its own distinct Bug without disturbing earlier linkage.
func TestCorrectiveExactlyOnce(t *testing.T) {
	bin, root, home, epicID, _ := setupCorrectiveAggregate(t)
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "failed", "release check failed", "--actor-role", "contractor"))
	bugID := report["corrective_bug_id"].(string)
	db := openCorrectiveDB(t, root)
	var links, requirements, bugs int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_corrective_bugs WHERE verification_report_id=?`, report["id"]).Scan(&links)
	_ = db.QueryRow(`SELECT COUNT(*) FROM requirements WHERE task_id=?`, bugID).Scan(&requirements)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=? AND type='bug'`, epicID).Scan(&bugs)
	if links != 1 || requirements != 1 || bugs != 1 {
		t.Fatalf("report did not link exactly one Bug: links=%d requirements=%d bugs=%d", links, requirements, bugs)
	}
	if _, err := db.Exec(`INSERT INTO work_item_corrective_bugs(verification_report_id,bug_work_item_id) VALUES(?,?)`, report["id"], "wi-"+shortID()); err == nil {
		t.Fatal("duplicate corrective-Bug link for the same report was accepted")
	}
	report2 := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "blocked", "second release blocked", "--actor-role", "contractor"))
	bug2ID := report2["corrective_bug_id"].(string)
	if bug2ID == bugID || bug2ID == "" {
		t.Fatalf("separate reports shared or lacked a corrective Bug: first=%s second=%q", bugID, bug2ID)
	}
	var totalBugs int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=? AND type='bug'`, epicID).Scan(&totalBugs)
	var firstLinks int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_corrective_bugs WHERE verification_report_id=?`, report["id"]).Scan(&firstLinks)
	if totalBugs != 2 || firstLinks != 1 {
		t.Fatalf("new report disturbed prior linkage: totalBugs=%d firstReportLinks=%d", totalBugs, firstLinks)
	}
}

// TestCorrectiveRetryDedup: retrying aggregate-verify for the same aggregate and
// status after an ambiguous response returns the existing linked corrective Bug
// instead of creating a duplicate Bug or report pair.
func TestCorrectiveRetryDedup(t *testing.T) {
	bin, root, home, epicID, _ := setupCorrectiveAggregate(t)
	first := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "failed", "release check failed", "--actor-role", "contractor"))
	bugID, _ := first["corrective_bug_id"].(string)
	if bugID == "" {
		t.Fatalf("first failed report produced no corrective Bug: %#v", first)
	}
	retry := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "failed", "release check failed", "--actor-role", "contractor"))
	retryBugID, _ := retry["corrective_bug_id"].(string)
	if retry["status"] != "failed" || retryBugID != bugID {
		t.Fatalf("retry did not return the existing corrective Bug: first=%s retry=%s report=%#v", bugID, retryBugID, retry)
	}
	if first["id"] == retry["id"] {
		t.Fatalf("retry reused the same report id: %s", first["id"])
	}
	db := openCorrectiveDB(t, root)
	var bugs, links int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=? AND type='bug'`, epicID).Scan(&bugs)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_corrective_bugs WHERE bug_work_item_id=?`, bugID).Scan(&links)
	if bugs != 1 || links != 1 {
		t.Fatalf("retry created a duplicate corrective Bug: bugs=%d links=%d", bugs, links)
	}
}

// TestCorrectiveImmutable: creating a corrective Bug never reopens or rewrites
// a completed descendant or its evidence.
func TestCorrectiveImmutable(t *testing.T) {
	bin, root, home, epicID, childID := setupCorrectiveAggregate(t)
	db := openCorrectiveDB(t, root)
	runSQLite(t, filepath.Join(root, ".pi", "tasks.db"), `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria,status) VALUES('req-imm','`+childID+`','REQ-IMM','Immutable','Given done When reported Then unchanged','pending')`)
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "failed", "release check failed", "--actor-role", "contractor"))
	if report["status"] != "failed" {
		t.Fatalf("immutable aggregate report = %#v", report)
	}
	var childStatus, reqStatus string
	_ = db.QueryRow(`SELECT status FROM work_items WHERE id=?`, childID).Scan(&childStatus)
	_ = db.QueryRow(`SELECT status FROM requirements WHERE id='req-imm'`).Scan(&reqStatus)
	if childStatus != "done" || reqStatus != "pending" {
		t.Fatalf("completed descendant changed: status=%q requirement=%q", childStatus, reqStatus)
	}
	var correctiveEventsOnChild int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type LIKE 'corrective%'`, childID).Scan(&correctiveEventsOnChild)
	if correctiveEventsOnChild != 0 {
		t.Fatalf("completed descendant received corrective evidence events: %d", correctiveEventsOnChild)
	}
	var aggregateStatus string
	_ = db.QueryRow(`SELECT status FROM work_items WHERE id=?`, epicID).Scan(&aggregateStatus)
	if aggregateStatus == "done" {
		t.Fatal("aggregate was closed even though verification failed")
	}
}
