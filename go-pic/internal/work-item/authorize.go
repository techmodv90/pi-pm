package workitem

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/earendil-works/task-system/go-pic/internal/tip"
	"os"
	"sort"
	"strings"
)

func ValidateTaskGraphRequirementCoverage(db Queryer, workItemID string, plan tip.TaskPlanDocument) (map[string]tip.RequirementSnapshot, error) {
	// Materialized children inherit requirement coverage from their root parent.
	requirements, err := queryMaps(db, `SELECT id,requirement_key,title,description,acceptance_criteria FROM requirements WHERE (epic_id=? OR task_id=? OR task_id IN (SELECT root_work_item_id FROM work_item_materializations WHERE work_item_id=?)) AND status!='deferred'`, workItemID, workItemID, workItemID)
	if err != nil {
		return nil, err
	}
	known, covered := map[string]tip.RequirementSnapshot{}, map[string]bool{}
	for _, requirement := range requirements {
		key := fmt.Sprint(requirement["requirement_key"])
		if err := validateGherkinSteps(fmt.Sprint(requirement["acceptance_criteria"])); err != nil {
			return nil, fmt.Errorf("%s acceptance criteria %w", key, err)
		}
		snapshot := tip.RequirementSnapshot{RequirementID: fmt.Sprint(requirement["id"]), RequirementKey: key, Title: fmt.Sprint(requirement["title"]), Description: fmt.Sprint(requirement["description"]), AcceptanceCriteria: fmt.Sprint(requirement["acceptance_criteria"])}
		snapshot.SourceHash = hashJSON(map[string]any{"id": snapshot.RequirementID, "key": snapshot.RequirementKey, "title": snapshot.Title, "description": snapshot.Description, "acceptance_criteria": snapshot.AcceptanceCriteria})
		known[strings.ToUpper(key)] = snapshot
	}
	for _, node := range plan.Nodes {
		if len(node.RequirementKeys) > 2 {
			return nil, fmt.Errorf("%s has more than two requirement_keys; split the node", node.Key)
		}
		for _, key := range node.RequirementKeys {
			normalized := strings.ToUpper(key)
			if _, ok := known[normalized]; !ok {
				return nil, fmt.Errorf("%s references unknown requirement %s", node.Key, key)
			}
			covered[normalized] = true
		}
	}
	missing := []string{}
	for normalized, requirement := range known {
		if !covered[normalized] {
			missing = append(missing, requirement.RequirementKey)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return nil, fmt.Errorf("task graph missing requirements: %s", strings.Join(missing, ", "))
	}
	return known, nil
}
func Authorize(db *sql.DB, args []string) error {
	if len(args) < 2 || args[1] == "" {
		return errors.New("usage: pic work-item authorize <id> <owner> [--branch-name <branch> --base-branch <branch> --base-commit <sha>]")
	}
	if err := ValidateWorkflowActor(args[1], "owner"); err != nil {
		return errors.New("implementation authorization requires actor_role=owner")
	}
	opts, err := parseOptions(args[2:])
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var checkpointID, content string
	if err = tx.QueryRow(`SELECT c.id,a.content FROM workflow_checkpoints c JOIN work_item_artifacts a ON a.id=c.artifact_id AND a.revision=c.artifact_revision AND a.content_hash=c.content_hash WHERE c.work_item_id=? AND c.stage='task_graph' AND c.decision_type='approved' ORDER BY c.artifact_revision DESC LIMIT 1`, args[0]).Scan(&checkpointID, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("current task graph is not approved")
		}
		return err
	}
	plan, err := tip.ParseTaskPlanJSON("```task-plan-json\n" + content + "\n```")
	if err != nil {
		return err
	}
	if _, err = ValidateTaskGraphRequirementCoverage(tx, args[0], plan); err != nil {
		return err
	}
	var materialized int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM work_item_materializations WHERE root_work_item_id=? AND checkpoint_id=?`, args[0], checkpointID).Scan(&materialized); err != nil {
		return err
	}
	if materialized == 0 {
		return errors.New("current task graph is not materialized")
	}
	if _, err = tx.Exec(`UPDATE implementation_authorizations SET revoked_at=datetime('now') WHERE work_item_id=? AND revoked_at='' AND task_graph_checkpoint_id!=?`, args[0], checkpointID); err != nil {
		return err
	}
	var authorizationID string
	err = tx.QueryRow(`SELECT id FROM implementation_authorizations WHERE work_item_id=? AND task_graph_checkpoint_id=? AND revoked_at='' ORDER BY created_at DESC,rowid DESC LIMIT 1`, args[0], checkpointID).Scan(&authorizationID)
	if errors.Is(err, sql.ErrNoRows) {
		authorizationID = "wiauth-" + shortID()
		if _, err = tx.Exec(`INSERT INTO implementation_authorizations(id,work_item_id,task_graph_checkpoint_id,authorized_by) VALUES(?,?,?,?)`, authorizationID, args[0], checkpointID, args[1]); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE implementation_authorizations SET revoked_at=datetime('now') WHERE work_item_id=? AND task_graph_checkpoint_id=? AND revoked_at='' AND id!=?`, args[0], checkpointID, authorizationID); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE work_item_instruction_packs SET status='stale',stale_at=datetime('now') WHERE status='active' AND checkpoint_id!=? AND work_item_id IN (SELECT work_item_id FROM work_item_materializations WHERE root_work_item_id=?)`, checkpointID, args[0]); err != nil {
		return err
	}

	var kind string
	if err = tx.QueryRow(`SELECT type FROM work_items WHERE id=?`, args[0]).Scan(&kind); err != nil {
		return err
	}
	var branchLabel, coordinationLabel, childCount int
	_ = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM work_item_labels WHERE work_item_id=? AND label='integration:branch')`, args[0]).Scan(&branchLabel)
	_ = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM work_item_labels WHERE work_item_id=? AND label='integration:coordination')`, args[0]).Scan(&coordinationLabel)
	if branchLabel != 0 && coordinationLabel != 0 {
		return errors.New("aggregate cannot have both integration:branch and integration:coordination labels")
	}
	_ = tx.QueryRow(`SELECT COUNT(*) FROM work_items WHERE parent_id=?`, args[0]).Scan(&childCount)
	mode := "coordination"
	if branchLabel != 0 || (coordinationLabel == 0 && (kind == "feature" || (contains([]string{"task", "bug", "chore"}, kind) && childCount > 0))) {
		mode = "branch"
	}
	var branchAncestor int
	if err = tx.QueryRow(`WITH RECURSIVE ancestors(id,parent_id) AS (
		SELECT id,parent_id FROM work_items WHERE id=(SELECT parent_id FROM work_items WHERE id=?)
		UNION ALL SELECT wi.id,wi.parent_id FROM work_items wi JOIN ancestors a ON wi.id=a.parent_id
	) SELECT EXISTS(SELECT 1 FROM ancestors a JOIN work_item_delivery_states d ON d.work_item_id=a.id WHERE d.integration_mode='branch')`, args[0]).Scan(&branchAncestor); err != nil {
		return err
	}
	if branchAncestor != 0 {
		if branchLabel != 0 {
			return errors.New("nested aggregate cannot own a branch beneath a branch-owning ancestor")
		}
		mode = "coordination"
	}
	branchName, baseBranch, baseCommit := "", "develop", ""
	if mode == "branch" {
		branchName, baseBranch, baseCommit = opts["branch-name"], opts["base-branch"], opts["base-commit"]
		if branchName == "" || baseBranch == "" || baseCommit == "" || branchName == "HEAD" || branchName == baseBranch {
			return errors.New("branch-owning aggregate authorization requires a non-base branch, base branch, and base commit")
		}
	}
	var existingMode, existingBranch, existingBase string
	deliveryErr := tx.QueryRow(`SELECT integration_mode,branch_name,base_branch FROM work_item_delivery_states WHERE work_item_id=?`, args[0]).Scan(&existingMode, &existingBranch, &existingBase)
	if deliveryErr == nil && (existingMode != mode || existingBranch != branchName || existingBase != baseBranch) {
		return errors.New("delivery authority is already bound to a different integration mode or branch")
	}
	if errors.Is(deliveryErr, sql.ErrNoRows) {
		if _, err = tx.Exec(`INSERT INTO work_item_delivery_states(work_item_id,integration_mode,branch_name,base_branch,base_commit) VALUES(?,?,?,?,?)`, args[0], mode, branchName, baseBranch, baseCommit); err != nil {
			return err
		}
	} else if deliveryErr != nil {
		return deliveryErr
	}
	activated := int64(0)
	if err = tx.Commit(); err != nil {
		return err
	}
	writeJSON(os.Stdout, map[string]any{"id": authorizationID, "work_item_id": args[0], "checkpoint_id": checkpointID, "activated": activated, "integration_mode": mode, "branch_name": branchName, "base_branch": baseBranch, "base_commit": baseCommit})
	return nil
}
func ValidateWorkflowActor(actual, expected string) error {
	if actual != expected {
		return errors.New("invalid actor role")
	}
	if os.Getenv("PI_TASK_AGENT_NAME") != "" {
		return errors.New("child agents cannot assume workflow authority")
	}
	return nil
}
