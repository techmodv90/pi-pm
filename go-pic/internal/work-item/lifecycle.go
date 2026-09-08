package workitem

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const ReadySQL = `wi.type IN ('task','bug','chore') AND wi.status='open' AND wi.deferred=0 AND wi.claimed_at='' AND ((
	SELECT COUNT(*) FROM work_item_instruction_packs p WHERE p.work_item_id=wi.id AND p.status='active'
)=1 OR EXISTS (
	SELECT 1 FROM work_item_materializations m JOIN implementation_authorizations a ON a.work_item_id=m.root_work_item_id AND a.task_graph_checkpoint_id=m.checkpoint_id AND a.revoked_at='' WHERE m.work_item_id=wi.id
) OR (
	-- Lean path: no legacy pipeline state at all; the description is the worker input.
	(SELECT COUNT(*) FROM work_item_instruction_packs p WHERE p.work_item_id=wi.id AND p.status='active')=0 AND NOT EXISTS (
		SELECT 1 FROM work_item_materializations m WHERE m.work_item_id=wi.id
	)
)) AND NOT EXISTS (
	SELECT 1 FROM work_item_relations r JOIN work_items blocker ON blocker.id=r.related_work_item_id WHERE r.work_item_id=wi.id AND r.relation_type='blocks' AND blocker.status!='done'
) AND NOT EXISTS (
	SELECT 1 FROM work_item_relations r JOIN work_items gate_item ON gate_item.id=r.related_work_item_id WHERE r.work_item_id=wi.id AND r.relation_type='gates' AND gate_item.status!='done'
)`

func AddRelation(db *sql.DB, args []string, relationType string) error {
	if len(args) != 2 {
		return errors.New("usage: pic work-item relate <work-item-id> <blocks|gates|related> <related-work-item-id>")
	}
	if !contains([]string{"blocks", "gates", "related"}, relationType) {
		return fmt.Errorf("invalid relation type: %s", relationType)
	}
	if _, err := ByID(db, args[0]); err != nil {
		return err
	}
	blocker, err := ByID(db, args[1])
	if err != nil {
		return err
	}
	if relationType == "gates" && blocker["type"] != "gate" {
		return fmt.Errorf("Work Item %s is not a gate", args[1])
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if relationType != "related" {
		var cycle int
		err = tx.QueryRow(`WITH RECURSIVE dependencies(id) AS (
			SELECT ? UNION SELECT r.related_work_item_id FROM work_item_relations r JOIN dependencies d ON r.work_item_id=d.id WHERE r.relation_type IN ('blocks','gates')
		) SELECT EXISTS(SELECT 1 FROM dependencies WHERE id=?)`, args[1], args[0]).Scan(&cycle)
		if err != nil {
			return err
		}
		if cycle != 0 {
			return errors.New("dependency cycle")
		}
	}
	_, err = tx.Exec(`INSERT INTO work_item_relations(id,work_item_id,relation_type,related_work_item_id) VALUES(?,?,?,?)`, "wir-"+shortID(), args[0], relationType, args[1])
	if err == nil {
		err = tx.Commit()
	}
	if err == nil {
		writeJSON(os.Stdout, map[string]any{"work_item_id": args[0], "relation_type": relationType, "related_work_item_id": args[1]})
	}
	return err
}
func Claim(db *sql.DB, args []string) error {
	if len(args) != 2 || args[1] == "" {
		return errors.New("usage: pic work-item claim <id> <claimant>")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind string
	if err = tx.QueryRow(`SELECT type FROM work_items WHERE id=?`, args[0]).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("Work Item %s not found", args[0])
		}
		return err
	}
	if kind == "epic" || kind == "feature" || kind == "gate" {
		return fmt.Errorf("%s Work Item is not executable", kind)
	}
	result, err := tx.Exec(`UPDATE work_items AS wi SET claimed_at=datetime('now'),claimed_by=? WHERE wi.id=? AND `+ReadySQL, args[1], args[0])
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("Work Item %s is not ready", args[0])
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

// Lean artifact stages: rri_t_scenarios is the one retained supplementary
// artifact (saved by the in-session contractor during /apm review; never
// gated by a workflow checkpoint).
func ExecutionReset(db *sql.DB, args []string) error {
	if len(args) != 2 || args[1] != "owner" {
		return errors.New("usage: pic work-item execution-reset <id> owner")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	item, err := byIDTx(tx, args[0])
	if err != nil {
		return err
	}
	if item["status"] == "cancelled" || item["status"] == "done" {
		return errors.New("execution reset requires a non-terminal Work Item")
	}
	var rootID, checkpointID string
	if err = tx.QueryRow(`SELECT root_work_item_id,checkpoint_id FROM work_item_materializations WHERE work_item_id=? ORDER BY rowid DESC LIMIT 1`, args[0]).Scan(&rootID, &checkpointID); err != nil {
		return errors.New("execution reset requires a materialized child Work Item")
	}
	var active int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM pipeline_runs WHERE task_id=? AND status IN ('claimed','running')`, args[0]).Scan(&active); err != nil {
		return err
	}
	if active != 0 {
		return errors.New("execution reset requires no active pipeline runs")
	}
	if _, err = tx.Exec(`UPDATE implementation_authorizations SET revoked_at=datetime('now') WHERE work_item_id=? AND revoked_at=''`, args[0]); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE work_item_instruction_packs SET status='stale',stale_at=datetime('now') WHERE work_item_id=? AND status='active'`, args[0]); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='',review_status='pending',review_notes='' WHERE id=?`, args[0]); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"root_work_item_id": rootID, "task_graph_checkpoint_id": checkpointID})
	if _, err = tx.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,summary,payload_json) VALUES(?,?,?,?,?,?)`, "wie-"+shortID(), args[0], "execution_reset", "owner", "Owner reset this child execution binding; Task Graph and sibling Work Items preserved", string(payload)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return outputOne(db, `SELECT `+Columns+` FROM work_items WHERE id=?`, args[0])
}
