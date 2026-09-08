package acceptance

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

var GherkinStep = regexp.MustCompile(`(?i)^\s*(?:[-*]\s*)?(feature|scenario(?: outline)?|given|when|then|and|but)(?:\s*:|\s+)`)

func ValidateGherkinSteps(text string) error {
	given, when, then := false, false, false
	for _, line := range strings.Split(text, "\n") {
		match := GherkinStep.FindStringSubmatch(line)
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

func ValidateBehavioralGherkin(text string) error {
	hasFeature := false
	scenario := false
	given, when, then := false, false, false
	finish := func() error {
		if !scenario {
			return nil
		}
		if !given || !when || !then {
			return fmt.Errorf("behavioral Gherkin scenario requires Given, When, and Then")
		}
		return nil
	}
	for _, line := range strings.Split(text, "\n") {
		match := GherkinStep.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		switch strings.ToLower(match[1]) {
		case "feature":
			hasFeature = true
		case "scenario", "scenario outline":
			if err := finish(); err != nil {
				return err
			}
			scenario, given, when, then = true, false, false, false
		case "given":
			given = scenario
		case "when":
			when = scenario
		case "then":
			then = scenario
		}
	}
	if !hasFeature || !scenario {
		return fmt.Errorf("behavioral Gherkin acceptance criteria require Feature and Scenario")
	}
	return finish()
}

func ValidateTaskGherkin(db *sql.DB, taskID, description string) error {
	criteria, err := TaskAcceptanceCriteria(db, taskID)
	if err != nil {
		return err
	}
	if description == "" {
		if err := db.QueryRow(`SELECT description FROM work_items WHERE id=?`, taskID).Scan(&description); err != nil {
			return err
		}
	}
	return ValidateBehavioralGherkin(description + "\n" + criteria)
}

func TaskAcceptanceCriteria(db *sql.DB, taskID string) (string, error) {
	rows, err := db.Query(`SELECT acceptance_criteria FROM requirements WHERE task_id=? AND status != 'deferred' ORDER BY requirement_key`, taskID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	parts := []string{}
	for rows.Next() {
		var criteria string
		if err := rows.Scan(&criteria); err != nil {
			return "", err
		}
		parts = append(parts, criteria)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(parts, "\n"), nil
}
