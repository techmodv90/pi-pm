package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type workflowStageRecord struct {
	attempts  int
	startedAt string
	endedAt   string
	status    string
	outcome   string
}

func projectWorkflowAnalytics(db *sql.DB) (map[string]any, error) {
	tasks, err := queryMaps(db, `SELECT t.id,t.title,COALESCE(e.title,'') AS epicTitle,t.workflow_mode AS workflowMode,t.status,t.review_status,t.review_notes,t.created_at AS createdAt FROM tasks t LEFT JOIN epics e ON e.id=t.epic_id ORDER BY datetime(t.created_at) DESC,t.rowid DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	events, err := queryMaps(db, `SELECT task_id,event_type,summary,created_at FROM task_events WHERE event_type IN ('scan_started','implementation_started','review_started','review_passed','review_failed','qa_started','verification_started') ORDER BY datetime(created_at) DESC,rowid DESC`)
	if err != nil {
		return nil, err
	}
	scans, err := queryMaps(db, `SELECT task_id,status,summary,created_at FROM scan_reports ORDER BY datetime(created_at) DESC,rowid DESC`)
	if err != nil {
		return nil, err
	}
	rri, err := queryMaps(db, `SELECT task_id,status,created_at,updated_at,completed_at FROM rri_sessions ORDER BY datetime(created_at) DESC,rowid DESC`)
	if err != nil {
		return nil, err
	}
	designs, err := queryMaps(db, `SELECT task_id,status,created_at,approved_at,rejected_at,rejection_reason FROM designs ORDER BY version DESC,rowid DESC`)
	if err != nil {
		return nil, err
	}
	completions, err := queryMaps(db, `SELECT task_id,status,summary,created_at FROM completion_reports ORDER BY datetime(created_at) DESC,rowid DESC`)
	if err != nil {
		return nil, err
	}
	verifications, err := queryMaps(db, `SELECT task_id,status,summary,verified_by_role,created_at FROM verification_reports ORDER BY datetime(created_at) DESC,rowid DESC`)
	if err != nil {
		return nil, err
	}

	starts := map[string]map[string]workflowStageRecord{}
	ends := map[string]map[string]workflowStageRecord{}
	for _, event := range events {
		taskID, eventType := fmt.Sprint(event["task_id"]), fmt.Sprint(event["event_type"])
		stage, terminal := eventStage(eventType)
		if stage == "" {
			continue
		}
		target := starts
		if terminal {
			target = ends
		}
		if target[taskID] == nil {
			target[taskID] = map[string]workflowStageRecord{}
		}
		record := target[taskID][stage]
		record.attempts++
		if record.startedAt == "" {
			record.startedAt = fmt.Sprint(event["created_at"])
			record.outcome = fmt.Sprint(event["summary"])
			if terminal {
				record.status = strings.TrimPrefix(eventType, "review_")
			}
		}
		target[taskID][stage] = record
	}

	scanStages := reportStages(scans, "")
	completionStages := reportStages(completions, "")
	verificationStages := reportStages(verifications, "")
	rriStages := rriStageRecords(rri)
	designStages := designStageRecords(designs)

	rows := []map[string]any{}
	for _, task := range tasks {
		taskID := fmt.Sprint(task["id"])
		stages := []struct {
			name   string
			label  string
			record workflowStageRecord
		}{
			{"scan", "Scan", scanStages[taskID]},
			{"rri", "RRI", rriStages[taskID]},
			{"design", "Design", designStages[taskID]},
			{"implementation", "Implementation", completionStages[taskID]},
			{"review", "Review", reviewStageRecord(task, starts[taskID]["review"], ends[taskID]["review"])},
			{"verification", "Contractor Verify", verificationStages[taskID]},
		}
		for index, stage := range stages {
			record := stage.record
			start := starts[taskID][stage.name]
			completedAttempts := record.attempts
			if record.startedAt == "" {
				record.startedAt = start.startedAt
			}
			if start.attempts > record.attempts {
				record.attempts = start.attempts
			}
			if start.attempts > completedAttempts || analyticsTimeAfter(start.startedAt, record.endedAt) {
				record.status = "running"
				record.endedAt = ""
				record.outcome = start.outcome
			}
			if record.status == "" {
				record.status = "pending"
				if record.startedAt != "" {
					record.status = "running"
				}
			}
			if stage.name == "design" && record.attempts == 0 && !contains([]string{"designed", "full"}, fmt.Sprint(task["workflowMode"])) {
				record.status = "skipped"
			}
			rows = append(rows, map[string]any{
				"taskId": taskID, "taskTitle": task["title"], "epicTitle": task["epicTitle"], "workflowMode": task["workflowMode"],
				"stage": stage.name, "stageLabel": stage.label, "stageOrder": index + 1, "status": record.status,
				"startedAt": record.startedAt, "completedAt": record.endedAt, "elapsedSeconds": elapsedSeconds(record.startedAt, record.endedAt, record.status),
				"attempts": record.attempts, "outcome": record.outcome,
			})
		}
	}
	return map[string]any{"rows": rows}, nil
}

func eventStage(eventType string) (string, bool) {
	switch eventType {
	case "scan_started":
		return "scan", false
	case "implementation_started":
		return "implementation", false
	case "review_started":
		return "review", false
	case "review_passed", "review_failed":
		return "review", true
	case "qa_started", "verification_started":
		return "verification", false
	default:
		return "", false
	}
}

func reportStages(rows []map[string]any, _ string) map[string]workflowStageRecord {
	stages := map[string]workflowStageRecord{}
	for _, row := range rows {
		taskID := fmt.Sprint(row["task_id"])
		record := stages[taskID]
		record.attempts++
		if record.endedAt == "" {
			record.endedAt = fmt.Sprint(row["created_at"])
			record.status = analyticsStatus(fmt.Sprint(row["status"]))
			record.outcome = fmt.Sprint(row["summary"])
		}
		stages[taskID] = record
	}
	return stages
}

func rriStageRecords(rows []map[string]any) map[string]workflowStageRecord {
	stages := map[string]workflowStageRecord{}
	for _, row := range rows {
		taskID := fmt.Sprint(row["task_id"])
		record := stages[taskID]
		record.attempts++
		if record.startedAt == "" {
			record.startedAt = fmt.Sprint(row["created_at"])
			status := fmt.Sprint(row["status"])
			record.status = map[string]string{"completed": "passed", "abandoned": "failed"}[status]
			if record.status == "" {
				record.status = "running"
			}
			if status == "completed" {
				record.endedAt = fmt.Sprint(row["completed_at"])
			}
			record.outcome = strings.ReplaceAll(status, "_", " ")
		}
		stages[taskID] = record
	}
	return stages
}

func designStageRecords(rows []map[string]any) map[string]workflowStageRecord {
	stages := map[string]workflowStageRecord{}
	for _, row := range rows {
		taskID := fmt.Sprint(row["task_id"])
		record := stages[taskID]
		record.attempts++
		if record.startedAt == "" {
			record.startedAt = fmt.Sprint(row["created_at"])
			status := fmt.Sprint(row["status"])
			record.status = map[string]string{"approved": "passed", "rejected": "failed", "superseded": "failed"}[status]
			if record.status == "" {
				record.status = "running"
			}
			if status == "approved" {
				record.endedAt = fmt.Sprint(row["approved_at"])
			} else if status == "rejected" {
				record.endedAt = fmt.Sprint(row["rejected_at"])
			}
			record.outcome = firstNonEmpty(fmt.Sprint(row["rejection_reason"]), status)
		}
		stages[taskID] = record
	}
	return stages
}

func reviewStageRecord(task map[string]any, start, end workflowStageRecord) workflowStageRecord {
	status := fmt.Sprint(task["review_status"])
	record := workflowStageRecord{attempts: start.attempts, startedAt: start.startedAt, endedAt: end.startedAt, outcome: firstNonEmpty(end.outcome, fmt.Sprint(task["review_notes"]))}
	if status != "" {
		record.status = map[string]string{"passed": "passed", "failed": "failed", "pending": "pending"}[status]
	}
	if record.status == "pending" && record.startedAt != "" && record.endedAt == "" {
		record.status = "running"
	}
	return record
}

func analyticsStatus(status string) string {
	switch status {
	case "completed", "done", "passed":
		return "passed"
	case "failed":
		return "failed"
	case "blocked":
		return "blocked"
	case "partial":
		return "partial"
	default:
		return status
	}
}

func elapsedSeconds(start, end, status string) any {
	started, ok := parseAnalyticsTime(start)
	if !ok {
		return nil
	}
	ended, ok := parseAnalyticsTime(end)
	if !ok {
		if status != "running" {
			return nil
		}
		ended = time.Now().UTC()
	}
	seconds := int(ended.Sub(started).Seconds())
	if seconds < 0 {
		return nil
	}
	return seconds
}

func analyticsTimeAfter(left, right string) bool {
	leftTime, leftOK := parseAnalyticsTime(left)
	rightTime, rightOK := parseAnalyticsTime(right)
	return leftOK && (!rightOK || leftTime.After(rightTime))
}

func parseAnalyticsTime(value string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, time.RFC3339Nano} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
