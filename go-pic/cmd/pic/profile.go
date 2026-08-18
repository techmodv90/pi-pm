package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Profile depth and lifecycle constants. The Plan profile selects the durable
// planning stages for a Work Item, the Implement and QA profiles select the
// execution stages. All profiles are persisted as version-bound rows resolved
// exactly once at planning start and reused for every later claim.
const workItemProfilesTableSQL = `CREATE TABLE IF NOT EXISTS work_item_profiles (
	id TEXT PRIMARY KEY,
	work_item_id TEXT NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
	profile_name TEXT NOT NULL CHECK(profile_name IN ('plan','implement','qa')),
	profile_version INTEGER NOT NULL CHECK(profile_version>0),
	planning_depth TEXT NOT NULL CHECK(planning_depth IN ('quick','standard','designed','full')),
	stages_json TEXT NOT NULL,
	content_hash TEXT NOT NULL,
	resolved_at TEXT DEFAULT (datetime('now')),
	UNIQUE(work_item_id, profile_name, profile_version)
)`

var lifecycleProfileNames = []string{"plan", "implement", "qa"}
var validPlanningDepths = []string{"quick", "standard", "designed", "full"}

type workItemProfile struct {
	Name          string
	Version       int
	PlanningDepth string
	Stages        []string
	ContentHash   string
}

func validPlanningDepth(depth string) bool {
	return contains(validPlanningDepths, depth)
}

// lifecycleForStage maps a pipeline stage onto its lifecycle profile name.
func lifecycleForStage(stage string) string {
	switch stage {
	case "scan", "rri", "vision", "blueprint", "contracts", "task_graph":
		return "plan"
	case "worker":
		return "implement"
	case "review", "autofix":
		return "qa"
	}
	return ""
}

// planStagesForProfile selects the durable Plan stages from the Work Item kind
// and its persisted planning depth. RRI and Task Graph are always present;
// Vision, Blueprint, and Contracts are only present for the depths that
// require them.
func planStagesForProfile(kind, parentID, depth string) []string {
	if contains([]string{"task", "bug", "chore"}, kind) && parentID == "" {
		return []string{"scan", "rri", "task_graph"}
	}
	switch depth {
	case "full":
		return []string{"scan", "rri", "vision", "blueprint", "contracts", "task_graph"}
	case "designed":
		return []string{"scan", "rri", "blueprint", "task_graph"}
	default: // quick, standard
		return []string{"scan", "rri", "task_graph"}
	}
}

func lifecycleStagesByName(name string, depth string, planStages []string) []string {
	switch name {
	case "implement":
		return []string{"worker"}
	case "qa":
		return []string{"review", "autofix"}
	default:
		return planStages
	}
}

func profileContentHash(name string, version int, depth string, stages []string) string {
	data, _ := json.Marshal(map[string]any{"name": name, "version": version, "planning_depth": depth, "stages": stages})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// workItemDepthInfo returns the persisted kind, parent, and planning depth for
// a Work Item, validating the depth value against the known set.
func workItemDepthInfo(db databaseQueryer, id string) (kind, parentID, depth string, err error) {
	if err = db.QueryRow(`SELECT type,COALESCE(parent_id,''),COALESCE(planning_depth,'full') FROM work_items WHERE id=?`, id).Scan(&kind, &parentID, &depth); err != nil {
		return "", "", "", err
	}
	if !validPlanningDepth(depth) {
		return "", "", "", fmt.Errorf("invalid persisted planning depth %q for Work Item %s", depth, id)
	}
	return kind, parentID, depth, nil
}

// computePlanStagesForWorkItem resolves the Plan profile stages for a Work Item
// without mutating the database. It prefers a persisted profile and falls back
// to deterministic type/depth resolution when no profile has been persisted yet.
func computePlanStagesForWorkItem(db databaseQueryer, id string) ([]string, string, int, string, error) {
	kind, parentID, depth, err := workItemDepthInfo(db, id)
	if err != nil {
		return nil, "", 0, "", err
	}
	stages := planStagesForProfile(kind, parentID, depth)
	var version int
	var hash string
	err = db.QueryRow(`SELECT profile_version,content_hash FROM work_item_profiles WHERE work_item_id=? AND profile_name='plan' AND profile_version=(SELECT COALESCE(MAX(profile_version),0) FROM work_item_profiles WHERE work_item_id=? AND profile_name='plan')`, id, id).Scan(&version, &hash)
	if err == nil && version > 0 {
		var stagesJSON string
		if err = db.QueryRow(`SELECT stages_json FROM work_item_profiles WHERE work_item_id=? AND profile_name='plan' AND profile_version=?`, id, version).Scan(&stagesJSON); err == nil {
			var persisted []string
			if json.Unmarshal([]byte(stagesJSON), &persisted) == nil && len(persisted) > 0 {
				stages = persisted
			}
		}
	}
	return stages, depth, version, hash, nil
}

// ensureWorkItemProfiles resolves the Plan, Implement, and QA profiles exactly
// once for a Work Item and returns them by name. Existing profile rows are
// reused so historical profile identity and lineage remain immutable.
func ensureWorkItemProfiles(tx *sql.Tx, id string) (map[string]workItemProfile, error) {
	kind, parentID, depth, err := workItemDepthInfo(tx, id)
	if err != nil {
		return nil, err
	}
	planStages := planStagesForProfile(kind, parentID, depth)
	profiles := map[string]workItemProfile{}
	for _, name := range lifecycleProfileNames {
		var version int
		if err = tx.QueryRow(`SELECT COALESCE(MAX(profile_version),0) FROM work_item_profiles WHERE work_item_id=? AND profile_name=?`, id, name).Scan(&version); err != nil {
			return nil, err
		}
		if version > 0 {
			var storedDepth, stagesJSON, hash string
			if err = tx.QueryRow(`SELECT planning_depth,stages_json,content_hash FROM work_item_profiles WHERE work_item_id=? AND profile_name=? AND profile_version=?`, id, name, version).Scan(&storedDepth, &stagesJSON, &hash); err != nil {
				return nil, err
			}
			var stages []string
			if err = json.Unmarshal([]byte(stagesJSON), &stages); err != nil || len(stages) == 0 {
				return nil, fmt.Errorf("corrupt persisted %s profile for Work Item %s", name, id)
			}
			for _, stage := range stages {
				if !contains(pipelineStages, stage) {
					return nil, fmt.Errorf("invalid stage %q in persisted %s profile for Work Item %s", stage, name, id)
				}
			}
			profiles[name] = workItemProfile{Name: name, Version: version, PlanningDepth: storedDepth, Stages: stages, ContentHash: hash}
			continue
		}
		stages := lifecycleStagesByName(name, depth, planStages)
		for _, stage := range stages {
			if !contains(pipelineStages, stage) {
				return nil, fmt.Errorf("unknown pipeline stage %q in %s profile", stage, name)
			}
		}
		profileHash := profileContentHash(name, version+1, depth, stages)
		stagesJSON, _ := json.Marshal(stages)
		if _, err = tx.Exec(`INSERT INTO work_item_profiles(id,work_item_id,profile_name,profile_version,planning_depth,stages_json,content_hash) VALUES(?,?,?,?,?,?,?)`, "wiprof-"+shortID(), id, name, version+1, depth, string(stagesJSON), profileHash); err != nil {
			return nil, err
		}
		profiles[name] = workItemProfile{Name: name, Version: version + 1, PlanningDepth: depth, Stages: stages, ContentHash: profileHash}
	}
	return profiles, nil
}

func workflowProfileList(db *sql.DB, args []string) error {
	if len(args) < 1 {
		return errors.New("workflow profile-list requires Work Item id")
	}
	if _, err := workItemByID(db, args[0]); err != nil {
		return err
	}
	rows, err := queryMaps(db, `SELECT * FROM work_item_profiles WHERE work_item_id=? ORDER BY profile_name,profile_version`, args[0])
	if err != nil {
		return err
	}
	writeJSON(os.Stdout, rows)
	return nil
}
