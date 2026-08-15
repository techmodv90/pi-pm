package main

import (
	"database/sql"
	"fmt"
	"sort"
)

type effectiveContractEntry struct {
	RequirementID   string `json:"requirement_id"`
	ContractKey     string `json:"contract_key"`
	RequirementHash string `json:"requirement_hash"`
	Status          string `json:"status"`
	OperationID     string `json:"operation_id,omitempty"`
	Provenance      string `json:"provenance"`
}

type effectiveContractSnapshot struct {
	TaskID      string                   `json:"task_id"`
	ContentHash string                   `json:"content_hash"`
	Entries     []effectiveContractEntry `json:"entries"`
}

func compileEffectiveContract(db *sql.DB, taskID string) (effectiveContractSnapshot, error) {
	return compileEffectiveContractWithRequirements(db, taskID, nil)
}

func compileEffectiveContractWithRequirements(db *sql.DB, taskID string, assignedRequirementIDs []string) (effectiveContractSnapshot, error) {
	task, err := queryOne(db, `SELECT id,epic_id FROM tasks WHERE id=?`, taskID)
	if err != nil {
		return effectiveContractSnapshot{}, fmt.Errorf("Task %s not found", taskID)
	}
	epicID := persistedText(task["epic_id"])
	requirements, err := queryMaps(db, `SELECT * FROM requirements WHERE task_id=? OR (epic_id=? AND inherit_to_descendants=1) ORDER BY requirement_key,id`, taskID, epicID)
	if err != nil {
		return effectiveContractSnapshot{}, err
	}
	entries := map[string]effectiveContractEntry{}
	for _, requirement := range requirements {
		id := persistedText(requirement["id"])
		provenance := "task"
		if persistedText(requirement["epic_id"]) != "" {
			provenance = "epic:inherited"
		}
		entries[id] = effectiveContractEntry{RequirementID: id, ContractKey: persistedText(requirement["contract_key"]), RequirementHash: requirementContentHash(requirement), Status: "effective", Provenance: provenance}
	}
	for _, id := range assignedRequirementIDs {
		if _, exists := entries[id]; exists {
			continue
		}
		requirement, err := queryOne(db, `SELECT * FROM requirements WHERE id=?`, id)
		if err != nil {
			return effectiveContractSnapshot{}, fmt.Errorf("Requirement %s not found", id)
		}
		entries[id] = effectiveContractEntry{RequirementID: id, ContractKey: persistedText(requirement["contract_key"]), RequirementHash: requirementContentHash(requirement), Status: "effective", Provenance: "task:assigned"}
	}
	operations, err := queryMaps(db, `SELECT * FROM contract_operations WHERE status='approved' AND (task_id=? OR (epic_id=? AND inherit_to_descendants=1)) ORDER BY created_at,id`, taskID, epicID)
	if err != nil {
		return effectiveContractSnapshot{}, err
	}
	edges := map[string]string{}
	for _, operation := range operations {
		if persistedText(operation["operation_type"]) != "replace" {
			continue
		}
		targets, targetErr := queryMaps(db, `SELECT requirement_id FROM contract_operation_targets WHERE operation_id=?`, operation["id"])
		if targetErr != nil {
			return effectiveContractSnapshot{}, targetErr
		}
		for _, target := range targets {
			edges[persistedText(target["requirement_id"])] = persistedText(operation["replacement_requirement_id"])
		}
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("contract replacement cycle at requirement %s", id)
		}
		if state[id] == 2 || edges[id] == "" {
			return nil
		}
		state[id] = 1
		if err := visit(edges[id]); err != nil {
			return err
		}
		state[id] = 2
		return nil
	}
	for id := range edges {
		if err := visit(id); err != nil {
			return effectiveContractSnapshot{}, err
		}
	}
	for _, operation := range operations {
		opID := persistedText(operation["id"])
		if persistedText(operation["owner_decision_id"]) == "" {
			return effectiveContractSnapshot{}, fmt.Errorf("approved contract operation %s lacks owner authorization", opID)
		}
		if persistedText(operation["operation_type"]) == "defer" {
			if persistedText(operation["reactivated_at"]) != "" {
				continue
			}
			if persistedText(operation["resume_condition"]) == "subject_completed" {
				table, id := "tasks", persistedText(operation["task_id"])
				if id == "" {
					table, id = "epics", persistedText(operation["epic_id"])
				}
				if done, _ := rowExists(db, `SELECT 1 FROM `+table+` WHERE id=? AND status='done'`, id); done {
					continue
				}
			}
		}
		targets, err := queryMaps(db, `SELECT requirement_id FROM contract_operation_targets WHERE operation_id=? ORDER BY requirement_id`, opID)
		if err != nil {
			return effectiveContractSnapshot{}, err
		}
		if len(targets) == 0 {
			return effectiveContractSnapshot{}, fmt.Errorf("contract operation %s has no targets", opID)
		}
		for _, target := range targets {
			id := persistedText(target["requirement_id"])
			entry, ok := entries[id]
			if !ok {
				requirement, requirementErr := queryOne(db, `SELECT * FROM requirements WHERE id=?`, id)
				if requirementErr != nil {
					return effectiveContractSnapshot{}, fmt.Errorf("contract operation %s targets missing requirement %s", opID, id)
				}
				entry = effectiveContractEntry{RequirementID: id, ContractKey: persistedText(requirement["contract_key"]), RequirementHash: requirementContentHash(requirement), Provenance: "resolution:target"}
			}
			entry.Status, entry.OperationID = "excluded", opID
			entries[id] = entry
		}
		if persistedText(operation["operation_type"]) == "replace" {
			id := persistedText(operation["replacement_requirement_id"])
			entry, ok := entries[id]
			if !ok {
				return effectiveContractSnapshot{}, fmt.Errorf("replacement requirement %s is outside task scope", id)
			}
			entry.Status, entry.OperationID = "effective", opID
			entries[id] = entry
		}
	}
	keys := map[string]string{}
	result := make([]effectiveContractEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Status == "effective" && entry.ContractKey != "" {
			if prior := keys[entry.ContractKey]; prior != "" {
				return effectiveContractSnapshot{}, fmt.Errorf("unresolved contract key %s: requirements %s and %s", entry.ContractKey, prior, entry.RequirementID)
			}
			keys[entry.ContractKey] = entry.RequirementID
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Status != result[j].Status {
			return result[i].Status == "effective"
		}
		return result[i].RequirementID < result[j].RequirementID
	})
	snapshot := effectiveContractSnapshot{TaskID: taskID, Entries: result}
	snapshot.ContentHash = hashJSON(snapshot.Entries)
	return snapshot, nil
}

func requirementContentHash(requirement map[string]any) string {
	return hashJSON(map[string]any{
		"id": requirement["id"], "key": requirement["requirement_key"], "contract_key": requirement["contract_key"],
		"title": requirement["title"], "description": requirement["description"], "acceptance_criteria": requirement["acceptance_criteria"],
	})
}
