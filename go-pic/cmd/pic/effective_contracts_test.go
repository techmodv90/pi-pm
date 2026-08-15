package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileEffectiveContractAppliesApprovedReplacement(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`INSERT INTO epics(id,title) VALUES('e-1','Epic');
		INSERT INTO tasks(id,epic_id,title) VALUES('t-1','e-1','Task');
		INSERT INTO requirements(id,epic_id,requirement_key,contract_key,title,inherit_to_descendants) VALUES('r-old','e-1','REQ-001','auth.strategy','Old',1);
		INSERT INTO requirements(id,task_id,requirement_key,contract_key,title) VALUES('r-new','t-1','REQ-002','auth.strategy','New');
		INSERT INTO owner_decisions(id,task_id,related_type,related_id,decision_type,decision) VALUES('od-1','t-1','contract_operation','op-1','approve_contract_operation','approved');
		INSERT INTO contract_operations(id,task_id,operation_type,status,replacement_requirement_id,completed_task_impact,owner_decision_id) VALUES('op-1','t-1','replace','approved','r-new','none','od-1');
		INSERT INTO contract_operation_targets(operation_id,requirement_id) VALUES('op-1','r-old')`)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := compileEffectiveContract(db, "t-1")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ContentHash == "" || len(snapshot.Entries) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Entries[0].RequirementID != "r-new" || snapshot.Entries[0].Status != "effective" {
		t.Fatalf("effective entry = %#v", snapshot.Entries[0])
	}
	if snapshot.Entries[1].RequirementID != "r-old" || snapshot.Entries[1].Status != "excluded" || snapshot.Entries[1].OperationID != "op-1" {
		t.Fatalf("excluded entry = %#v", snapshot.Entries[1])
	}
}

func TestCompileEffectiveContractRejectsDuplicateKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	if err := initDB(dbPath); err != nil {
		t.Fatal(err)
	}
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO tasks(id,title) VALUES('t-1','Task');
		INSERT INTO requirements(id,task_id,requirement_key,contract_key,title) VALUES('r-1','t-1','REQ-001','auth.strategy','One');
		INSERT INTO requirements(id,task_id,requirement_key,contract_key,title) VALUES('r-2','t-1','REQ-002','auth.strategy','Two')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = compileEffectiveContract(db, "t-1")
	if err == nil || !strings.Contains(err.Error(), "unresolved contract key auth.strategy") {
		t.Fatalf("error = %v", err)
	}
}
