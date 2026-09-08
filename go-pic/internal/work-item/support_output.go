package workitem

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var gherkinStep = regexp.MustCompile(`(?i)^\s*(?:[-*]\s*)?(feature|scenario(?: outline)?|given|when|then|and|but)(?:\s*:|\s+)`)

// ValidPlanningDepths is the persisted planning_depth vocabulary; profile
// selection (cmd/pic) shares it so both layers validate identically.
var ValidPlanningDepths = []string{"quick", "standard", "designed", "full"}

func ValidPlanningDepth(depth string) bool {
	return contains(ValidPlanningDepths, depth)
}

// validateGherkinSteps enforces the owner-facing acceptance contract: every
// requirement must carry separate Given, When, and Then steps.
func validateGherkinSteps(text string) error {
	given, when, then := false, false, false
	for _, line := range strings.Split(text, "\n") {
		match := gherkinStep.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		switch strings.ToLower(match[1]) {
		case "given":
			given = true
		case "when":
			when = true
		case "then":
			then = true
		}
	}
	if !given || !when || !then {
		return fmt.Errorf("require Given, When, and Then steps")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func queryOne(db Queryer, query string, args ...any) (map[string]any, error) {
	rows, err := queryMaps(db, query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return rows[0], nil
}

func outputOne(db Queryer, query string, args ...any) error {
	row, err := queryOne(db, query, args...)
	if err != nil {
		return err
	}
	writeJSON(os.Stdout, row)
	return nil
}

func addEvent(db Execer, workItemID, eventType, role, summary string, payload any) error {
	return addEventWithModel(db, workItemID, eventType, role, "", summary, payload)
}

func addEventWithModel(db Execer, workItemID, eventType, role, model, summary string, payload any) error {
	data, _ := json.Marshal(payload)
	_, err := db.Exec(`INSERT INTO work_item_events(id,work_item_id,event_type,actor_role,actor_model,summary,payload_json) VALUES(?,?,?,?,?,?,?)`, "wie-"+shortID(), workItemID, eventType, role, model, summary, string(data))
	return err
}
