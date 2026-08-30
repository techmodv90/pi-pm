package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProfileLifecycleIntegration threads the complete persisted profile,
// scheduler, authority, standalone, corrective, and promotion flows across the
// real temporary SQLite CLI harness (production command boundaries only). The
// harness environment may export PI_TASK_AGENT_NAME (a launched worker), which
// the pic binary treats as an authority override that blocks all lifecycle
// mutation; the integration fixture clears it so it drives the canonical
// owner/contractor/scheduler lifecycle rather than a worker role, then exercises
// authority rejection explicitly with the variable present.
func TestProfileLifecycleIntegration(t *testing.T) {
	t.Setenv("PI_TASK_AGENT_NAME", "")
	bin := buildPic(t)
	root, home := initProject(t, bin)

	// One aggregate fixture drives depth resolution, durable planning, child
	// materialization, implementation, aggregate verification, and merge
	// eligibility; one standalone fixture proves the lean identity path; negative
	// fixtures prove authority and history boundaries.
	t.Run("aggregate-full-lifecycle", func(t *testing.T) { integrationAggregateFullLifecycle(t, bin, root, home) })
	t.Run("standalone-lean-lifecycle", func(t *testing.T) { integrationStandaloneLifecycle(t, bin, root, home) })
	t.Run("authority-boundaries", func(t *testing.T) { integrationAuthorityBoundaries(t, bin, root, home) })
	t.Run("corrective-and-immutable", func(t *testing.T) { integrationCorrectiveAndImmutable(t, bin, root, home) })
	t.Run("stale-handoff-rejected", func(t *testing.T) { integrationStaleHandoff(t, bin, root, home) })
}

// integrationMaterializedChildID returns the materialized work item id for a
// node key under a root aggregate, or fails.
func integrationMaterializedChildID(t *testing.T, dbPath, rootID, nodeKey string) string {
	t.Helper()
	var id string
	if err := openSQLiteGo(t, dbPath).QueryRow(`SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=? AND node_key=?`, rootID, nodeKey).Scan(&id); err != nil {
		t.Fatalf("materialized %s/%s: %v", rootID, nodeKey, err)
	}
	return id
}

// integrationSeedExecutionEvidence marks a claimed worker run completed and
// integrated and inserts a passed review run bound to it, mirroring the
// established canonical evidence fixture so completion-save and
// verification-save can run through the CLI.
func integrationSeedExecutionEvidence(t *testing.T, dbPath, id, workerRunID string) {
	t.Helper()
	suffix := strings.ReplaceAll(id, "-", "")
	reviewRun := "pr-" + suffix + "-review"
	runSQLite(t, dbPath, `
		UPDATE pipeline_runs SET status='completed',artifact_saved_at=datetime('now'),integrated_patch_path='candidate-`+suffix+`.patch',integrated_patch_hash='patch-`+suffix+`',integrated_at=datetime('now'),completed_at=datetime('now') WHERE id='`+workerRunID+`';
		INSERT INTO pipeline_runs(id,task_id,stage,attempt,status,lease_token,lease_expires_at,instruction_pack_id,instruction_pack_version,instruction_pack_hash,candidate_run_id,candidate_patch_hash,result_json,completed_at)
		SELECT '`+reviewRun+`',task_id,'review',1,'completed','lease-`+suffix+`-review',datetime('now','+1 hour'),instruction_pack_id,instruction_pack_version,instruction_pack_hash,'`+workerRunID+`','patch-`+suffix+`','{"review_status":"passed","candidate_run_id":"`+workerRunID+`","candidate_patch_hash":"patch-`+suffix+`"}',datetime('now')
		FROM pipeline_runs WHERE id='`+workerRunID+`';`)
}

// integrationCompleteExecutable drives a dependency-ready executable (standalone
// root or materialized child) through worker claim, passed review, integrated
// completion, and passed contractor verification until it closes as done.
func integrationCompleteExecutable(t *testing.T, bin, root, home, id string) {
	t.Helper()
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	suffix := strings.ReplaceAll(id, "-", "")
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	workerRunID := claim["id"].(string)
	integrationSeedExecutionEvidence(t, dbPath, id, workerRunID)
	reviewRun := "pr-" + suffix + "-review"
	asObject(t, runPic(t, bin, root, home, "work-item", "review", id, "passed", "--notes", "candidate accepted", "--pipeline-run-id", reviewRun))
	completion := asObject(t, runPic(t, bin, root, home, "work-item", "completion-save", id, "done", "--pipeline-run-id", workerRunID, "--summary", "integrated"))
	asObject(t, runPic(t, bin, root, home, "work-item", "verification-save", id, completion["id"].(string), "passed", "checks passed", "--actor-role", "contractor"))
}

// integrationEnsureProfiles resolves and persists the Plan, Implement, and QA
// lifecycle profiles for a Work Item exactly once, so a promotion candidate's
// persisted profile identity exists before the production entrypoint runs.
func integrationEnsureProfiles(t *testing.T, dbPath, id string) {
	t.Helper()
	db := openSQLiteGo(t, dbPath)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ensureWorkItemProfiles(tx, id); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// integrationAggregateGraph is a strict_sequential two-node aggregate DAG with a
// dependency edge, matching the canonical schema-v3 graph used elsewhere.
func integrationAggregateGraph() string {
	return `{"version":3,"execution_policy":"strict_sequential","nodes":[
		{"key":"T01","type":"task","name":"Child A","goal":"Implement A","requirement_keys":["REQ-001"],"depends_on":[],"priority":"P1","module":"core","files":["a.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["a.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]},
		{"key":"T02","type":"task","name":"Child B","goal":"Implement B","requirement_keys":["REQ-001"],"depends_on":["T01"],"priority":"P1","module":"core","files":["b.go"],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["b.go"]},"verification":[{"command":"go test ./...","required":true}],"skillFamilies":[]}
	]}`
}

// integrationAssertPromotionEligible runs the production promotion entrypoint
// with per-stage, corrective failure-path, and gap-ledger evidence and asserts
// the candidate profile is eligible once the real lifecycle passed and was
// accepted (the lifecycle is reconciled by the production flow, never a
// caller-supplied fabrication).
func integrationAssertPromotionEligible(t *testing.T, bin, root, home, workItemID string) {
	t.Helper()
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	integrationEnsureProfiles(t, dbPath, workItemID)
	db := openSQLiteGo(t, dbPath)
	var profileID, profileHash, stagesJSON string
	if err := db.QueryRow(`SELECT id,content_hash,stages_json FROM work_item_profiles WHERE work_item_id=? AND profile_name='plan' ORDER BY profile_version DESC LIMIT 1`, workItemID).Scan(&profileID, &profileHash, &stagesJSON); err != nil {
		t.Fatal(err)
	}
	var stages []string
	if err := json.Unmarshal([]byte(stagesJSON), &stages); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	stagesEv := []map[string]any{}
	for _, stage := range stages {
		stagesEv = append(stagesEv, map[string]any{
			"stage":   stage,
			"success": map[string]any{"id": "pr-" + stage + "-pass", "artifact_id": "art-" + stage + "-pass", "outcome": "passed", "recorded_at": now},
			"failure": map[string]any{"id": "pr-" + stage + "-fail", "artifact_id": "art-" + stage + "-fail", "outcome": "failed", "recorded_at": now},
		})
	}
	payload, _ := json.Marshal(map[string]any{
		"profile_id":           profileID,
		"profile_content_hash": profileHash,
		"stages":               stagesEv,
		"corrective": map[string]any{
			"bug_run_id": "wi-corr-integration",
			"passed":     map[string]any{"id": "pr-corr-pass", "artifact_id": "art-corr-pass", "outcome": "passed", "recorded_at": now},
			"partial":    map[string]any{"id": "pr-corr-partial", "artifact_id": "art-corr-partial", "outcome": "partial", "recorded_at": now},
			"blocked":    map[string]any{"id": "pr-corr-blocked", "artifact_id": "art-corr-blocked", "outcome": "blocked", "recorded_at": now},
			"failed":     map[string]any{"id": "pr-corr-failed", "artifact_id": "art-corr-failed", "outcome": "failed", "recorded_at": now},
		},
		"invariants": []map[string]any{
			{"key": "REQ-001", "red_id": "red-REQ-001", "green_id": "green-REQ-001", "review_id": "review-REQ-001"},
		},
	})
	out := runMarkdown(t, bin, root, home, "workflow", "profile-promotion-evaluate", workItemID, "plan", "--evidence", string(payload))
	if !strings.Contains(out, `"eligible":true`) {
		t.Fatalf("reusable profile promotion not eligible after real lifecycle: %s", out)
	}
}

func integrationAggregateFullLifecycle(t *testing.T, bin, root, home string) {
	t.Helper()
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Aggregate integration", "--planning-depth", "designed"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-agg','`+id+`','REQ-001','Required','Given valid aggregate
When work runs
Then it completes')`)

	// REQ-AGGREGATE-DEPTH: RRI and Task Graph are mandatory; Vision and
	// Contracts are excluded for the selected designed depth.
	stages, _, _, _, err := computePlanStagesForWorkItem(openSQLiteGo(t, dbPath), id)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(stages, "rri") || !contains(stages, "task_graph") {
		t.Fatalf("aggregate depth missing mandatory rri/task_graph: %v", stages)
	}
	if contains(stages, "vision") || contains(stages, "contracts") {
		t.Fatalf("designed aggregate depth leaked aggregate-only stages: %v", stages)
	}

	// Durable planning through the CLI artifacts.
	planAggregateThroughGraph(t, bin, root, home, id, []string{"scan", "rri", "blueprint"}, integrationAggregateGraph())
	mat := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	if mat["created"] != float64(2) || mat["total"] != float64(2) {
		t.Fatalf("all aggregate nodes materialized: %#v", mat)
	}
	auth := asObject(t, runPic(t, bin, root, home, "work-item", "authorize", id, "owner"))
	if auth["work_item_id"] != id {
		t.Fatalf("owner authorization = %#v", auth)
	}

	// Implementation: children complete in dependency order through the CLI.
	childA := integrationMaterializedChildID(t, dbPath, id, "T01")
	childB := integrationMaterializedChildID(t, dbPath, id, "T02")
	integrationCompleteExecutable(t, bin, root, home, childA)
	integrationCompleteExecutable(t, bin, root, home, childB)
	db := openSQLiteGo(t, dbPath)
	var aDone, bDone int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE id=? AND status='done'`, childA).Scan(&aDone)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE id=? AND status='done'`, childB).Scan(&bDone)
	if aDone != 1 || bDone != 1 {
		t.Fatalf("aggregate children not done: A=%d B=%d", aDone, bDone)
	}

	// Aggregate verification (passed) creates no corrective Bug and the real
	// lifecycle becomes merge-eligible on owner acceptance.
	passed := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", id, "passed", "delivery verified", "--actor-role", "contractor"))
	if passed["status"] != "passed" || passed["corrective_bug_id"] != "" {
		t.Fatalf("passed aggregate verification = %#v", passed)
	}
	accepted := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-accept", id, passed["id"].(string), "accepted", "accepted", "--actor-role", "owner"))
	if accepted["decision"] != "accepted" {
		t.Fatalf("aggregate acceptance = %#v", accepted)
	}

	// REQ-PROMOTION-GATE: per-stage, corrective failure-path, and gap-ledger
	// evidence with the reconciled real lifecycle are eligible.
	integrationAssertPromotionEligible(t, bin, root, home, id)
}

func integrationStandaloneLifecycle(t *testing.T, bin, root, home string) {
	t.Helper()
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Standalone integration"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria) VALUES('req-stand','`+id+`','REQ-001','Required','Given valid standalone
When work runs
Then it completes')`)

	// REQ-STANDALONE-PATH: the lean profile is fixed regardless of the persisted
	// depth and retains Work Item identity.
	stages, err := planningStagesForWorkItem(openSQLiteGo(t, dbPath), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 3 || stages[0] != "scan" || stages[1] != "rri" || stages[2] != "task_graph" {
		t.Fatalf("standalone lean profile = %v", stages)
	}
	planStandaloneThroughGraph(t, bin, root, home, id, standaloneOneNodeGraph("task", "Implement standalone"))
	mat := asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))
	if mat["work_item_id"] != id || mat["created"] != float64(0) || mat["total"] != float64(1) {
		t.Fatalf("standalone materialization = %#v", mat)
	}
	asObject(t, runPic(t, bin, root, home, "work-item", "authorize", id, "owner"))

	// First Worker claim freezes the one-TIP lineage and retains identity.
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "worker"))
	if fmt.Sprint(claim["instruction_pack_id"]) == "" {
		t.Fatalf("standalone first claim froze no TIP: %#v", claim)
	}
	db := openSQLiteGo(t, dbPath)
	var activePacks int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_instruction_packs WHERE work_item_id=? AND status='active'`, id).Scan(&activePacks)
	if activePacks != 1 {
		t.Fatalf("standalone TIP lineage not singular: %d", activePacks)
	}

	// Implementation closure through the CLI.
	suffix := strings.ReplaceAll(id, "-", "")
	integrationSeedExecutionEvidence(t, dbPath, id, claim["id"].(string))
	reviewRun := "pr-" + suffix + "-review"
	asObject(t, runPic(t, bin, root, home, "work-item", "review", id, "passed", "--notes", "candidate accepted", "--pipeline-run-id", reviewRun))
	completion := asObject(t, runPic(t, bin, root, home, "work-item", "completion-save", id, "done", "--pipeline-run-id", claim["id"].(string), "--summary", "integrated"))
	asObject(t, runPic(t, bin, root, home, "work-item", "verification-save", id, completion["id"].(string), "passed", "checks passed", "--actor-role", "contractor"))
	var status string
	if err := db.QueryRow(`SELECT status FROM work_items WHERE id=?`, id).Scan(&status); err != nil || status != "done" {
		t.Fatalf("standalone implementation closure: status=%q err=%v", status, err)
	}
	// No child decomposition took place.
	var children int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, id).Scan(&children)
	if children != 0 {
		t.Fatalf("standalone produced %d children", children)
	}
}

func integrationAuthorityBoundaries(t *testing.T, bin, root, home string) {
	t.Helper()
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Authority", "--planning-depth", "designed"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-auth','`+id+`','REQ-001','Required','Given owned context
When a stranger acts
Then it is rejected')`)
	planAggregateThroughGraph(t, bin, root, home, id, []string{"scan", "rri", "blueprint"}, integrationAggregateGraph())
	asObject(t, runPic(t, bin, root, home, "work-item", "materialize", id))

	// A non-owner actor cannot authorize execution; a non-contractor actor cannot
	// run aggregate verification.
	if out := runPicError(t, bin, root, home, "work-item", "authorize", id, "contractor"); !strings.Contains(out, "actor_role=owner") {
		t.Fatalf("non-owner authorization accepted: %s", out)
	}
	if out := runPicError(t, bin, root, home, "work-item", "aggregate-verify", id, "passed", "verify", "--actor-role", "owner"); !strings.Contains(out, "actor_role=contractor") {
		t.Fatalf("non-contractor aggregate verification accepted: %s", out)
	}
	// A child agent (worker role) cannot mutate the Work Item lifecycle through pic.
	child := exec.Command(bin, "work-item", "status", id, "done")
	child.Dir = root
	child.Env = append(clearedPiEnv(), "HOME="+home, "PI_TASK_AGENT_NAME=task-worker")
	if out, err := child.CombinedOutput(); err == nil || !strings.Contains(string(out), "cannot mutate Work Item lifecycle") {
		t.Fatalf("child agent bypassed authority: err=%v out=%s", err, out)
	}
	// REQ-AUTHORITY-BOUNDARIES: the canonical owner gate permits the owner alone.
	auth := asObject(t, runPic(t, bin, root, home, "work-item", "authorize", id, "owner"))
	if auth["work_item_id"] != id {
		t.Fatalf("owner authorization = %#v", auth)
	}
}

func integrationCorrectiveAndImmutable(t *testing.T, bin, root, home string) {
	t.Helper()
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Corrective integration"))
	epicID := epic["id"].(string)
	child := asObject(t, runPic(t, bin, root, home, "work-item", "create", "task", "Completed child", "--parent", epicID))
	childID := child["id"].(string)
	runPic(t, bin, root, home, "work-item", "status", childID, "done")
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,task_id,requirement_key,title,acceptance_criteria,status) VALUES('req-imm','`+childID+`','REQ-IMM','Immutable','Given done When reported Then unchanged','pending')`)

	// REQ-CORRECTIVE-BUG-POLICY: a failed report waits for explicit owner
	// approval (owner_approval_required=1) and links exactly one Bug.
	report := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "failed", "release check failed", "--actor-role", "contractor"))
	bugID := report["corrective_bug_id"].(string)
	if report["status"] != "failed" || bugID == "" {
		t.Fatalf("failed aggregate report = %#v", report)
	}
	db := openSQLiteGo(t, dbPath)
	var approvalRequired int
	if err := db.QueryRow(`SELECT owner_approval_required FROM work_item_corrective_bugs WHERE verification_report_id=?`, report["id"]).Scan(&approvalRequired); err != nil || approvalRequired != 1 {
		t.Fatalf("failed corrective awaits owner approval: required=%d err=%v", approvalRequired, err)
	}

	// REQ-IMMUTABLE-HISTORY: a completed descendant and its artifact remain
	// unchanged when corrective recovery is initiated.
	var childStatus, reqStatus string
	_ = db.QueryRow(`SELECT status FROM work_items WHERE id=?`, childID).Scan(&childStatus)
	_ = db.QueryRow(`SELECT status FROM requirements WHERE id='req-imm'`).Scan(&reqStatus)
	if childStatus != "done" || reqStatus != "pending" {
		t.Fatalf("completed descendant changed during corrective: status=%q requirement=%q", childStatus, reqStatus)
	}
	var childEvents int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_events WHERE work_item_id=? AND event_type LIKE 'corrective%'`, childID).Scan(&childEvents)
	if childEvents != 0 {
		t.Fatalf("completed descendant received corrective evidence events: %d", childEvents)
	}

	// Duplicate corrective-Bug creation for the same report is rejected, and
	// retrying the same status returns the existing Bug (exactly-once).
	if _, err := db.Exec(`INSERT INTO work_item_corrective_bugs(verification_report_id,bug_work_item_id) VALUES(?,?)`, report["id"], "wi-"+shortID()); err == nil {
		t.Fatal("duplicate corrective-Bug link for the same report was accepted")
	}
	retry := asObject(t, runPic(t, bin, root, home, "work-item", "aggregate-verify", epicID, "failed", "release check failed", "--actor-role", "contractor"))
	if retry["corrective_bug_id"] != bugID {
		t.Fatalf("corrective retry did not dedup to the existing Bug: first=%s retry=%s", bugID, retry["corrective_bug_id"])
	}
	var bugs, links int
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=? AND type='bug'`, epicID).Scan(&bugs)
	_ = db.QueryRow(`SELECT COUNT(*) FROM work_item_corrective_bugs WHERE bug_work_item_id=?`, bugID).Scan(&links)
	if bugs != 1 || links != 1 {
		t.Fatalf("corrective exactly-once violated: bugs=%d links=%d", bugs, links)
	}
}

func integrationStaleHandoff(t *testing.T, bin, root, home string) {
	t.Helper()
	item := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Stale handoff"))
	id := item["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	runSQLite(t, dbPath, `INSERT INTO requirements(id,epic_id,requirement_key,title,acceptance_criteria) VALUES('req-stale','`+id+`','REQ-001','Required','Given stale claim When reused Then it is rejected')`)
	claim := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "scan"))
	if claim["stage"] != "scan" || persistedText(claim["profile_hash"]) == "" {
		t.Fatalf("scan claim did not bind profile: %#v", claim)
	}
	runSQLite(t, dbPath, `INSERT INTO work_item_artifacts(id,work_item_id,stage,revision,content,content_hash) VALUES('wia-stale','`+id+`','scan',1,'<scan/>','hash-stale'); INSERT INTO workflow_checkpoints(id,work_item_id,stage,artifact_id,artifact_revision,content_hash,decision_type) VALUES('wic-stale','`+id+`','scan','wia-stale',1,'hash-stale','accepted');`)
	// A stale persisted profile hash is not silently retried.
	if out := runPicError(t, bin, root, home, "workflow", "pipeline-claim", id, "rri", "--profile-hash", "deadbeef"); !strings.Contains(out, "profile hash changed") {
		t.Fatalf("stale profile hash accepted: %s", out)
	}
	// The correct bound hash is accepted for the next eligible stage.
	bound := asObject(t, runPic(t, bin, root, home, "workflow", "pipeline-claim", id, "rri", "--profile-hash", persistedText(claim["profile_hash"])))
	if bound["stage"] != "rri" {
		t.Fatalf("re-bound rri claim = %#v", bound)
	}
}

// clearedPiEnv returns the current process environment with worker-role override
// variables removed so a spawned pic runs under canonical authority only.
func clearedPiEnv() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "PI_TASK_AGENT_NAME=") {
			continue
		}
		env = append(env, entry)
	}
	return env
}
