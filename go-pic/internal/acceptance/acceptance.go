package acceptance

// Promotion evidence model. The promotion gate rejects reusable-profile
// promotion until every required evidence category is current and complete.
// Per the promotion-evidence contract, every reference is an immutable
// identifier (run/artifact/checkpoint/report/test) rather than a summary claim.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/earendil-works/task-system/go-pic/internal/profile"
	"github.com/earendil-works/task-system/go-pic/internal/store"
	"github.com/earendil-works/task-system/go-pic/internal/work-item"
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

// Promotion evidence model. The promotion gate rejects reusable-profile
// promotion until every required evidence category is current and complete.
// Per the promotion-evidence contract, every reference is an immutable
// identifier (run/artifact/checkpoint/report/test) rather than a summary claim.

// Outcomes are the lifecycle outcomes a stage or corrective run may
// carry. Success-path evidence requires `passed`; failure-path evidence covers
// `partial`, `blocked`, or `failed`.
var Outcomes = []string{"passed", "partial", "blocked", "failed"}

// run evidence types.
type RunEvidence struct {
	ID           string `json:"id"` // immutable pipeline run id (pr-*)
	ArtifactID   string `json:"artifact_id"`
	Outcome      string `json:"outcome"`
	RecordedAt   string `json:"recorded_at"`
	SupersededAt string `json:"superseded_at,omitempty"`
}

type StageEvidence struct {
	Stage   string       `json:"stage"`
	Success *RunEvidence `json:"success,omitempty"`
	Failure *RunEvidence `json:"failure,omitempty"`
}

type CorrectiveEvidence struct {
	BugRunID        string       `json:"bug_run_id"` // the one linked corrective Bug
	DuplicateBugIDs []string     `json:"duplicate_bug_ids,omitempty"`
	Passed          *RunEvidence `json:"passed,omitempty"`
	Partial         *RunEvidence `json:"partial,omitempty"`
	Blocked         *RunEvidence `json:"blocked,omitempty"`
	Failed          *RunEvidence `json:"failed,omitempty"`
}

type InvariantEvidence struct {
	Key      string `json:"key"` // REQ-* requirement key
	RedID    string `json:"red_id"`
	GreenID  string `json:"green_id"`
	ReviewID string `json:"review_id"`
}

type LifecycleEvidence struct {
	RunID        string `json:"run_id"`
	AggregateID  string `json:"aggregate_id"`
	CheckpointID string `json:"checkpoint_id"`
	ReportID     string `json:"report_id"`
	MergeID      string `json:"merge_id"`
	Outcome      string `json:"outcome"`
	Superseded   bool   `json:"superseded,omitempty"`
}

// Evidence is the parsed promotion evidence payload for one candidate
// reusable pipeline profile.
type Evidence struct {
	ProfileID          string              `json:"profile_id"`
	ProfileContentHash string              `json:"profile_content_hash"`
	Stages             []StageEvidence     `json:"stages"`
	Corrective         CorrectiveEvidence  `json:"corrective"`
	Invariants         []InvariantEvidence `json:"invariants"`
	Lifecycle          *LifecycleEvidence  `json:"lifecycle,omitempty"`
}

type Decision struct {
	Eligible  bool
	ProfileID string
	Missing   []string // missing evidence categories
	Rejected  []string // rejection reasons for present-but-invalid evidence
}

// promotionEvidenceParse is a hard pre-gate: a candidate profile whose
// promotion evidence fails to parse is never implicitly eligible.
func ParseEvidence(payload []byte) (*Evidence, error) {
	ev := &Evidence{}
	if err := json.Unmarshal(payload, ev); err != nil {
		return nil, fmt.Errorf("promotion evidence parse failure: %w", err)
	}
	if ev.ProfileID == "" {
		return nil, fmt.Errorf("promotion evidence parse failure: missing profile_id")
	}
	return ev, nil
}

func RunCurrent(ev *RunEvidence, now time.Time, maxAge time.Duration) bool {
	if ev == nil || ev.ID == "" || ev.ArtifactID == "" || !store.Contains(Outcomes, ev.Outcome) {
		return false
	}
	if ev.SupersededAt != "" {
		return false
	}
	recorded, err := time.Parse(time.RFC3339, ev.RecordedAt)
	if err != nil || now.Sub(recorded) > maxAge || now.Sub(recorded) < 0 {
		return false
	}
	return true
}

func FailurePath(outcome string) bool {
	return outcome == "partial" || outcome == "blocked" || outcome == "failed"
}

// EvaluateEligibility applies the strict promotion decision recorded
// in RRI: it returns the set of missing or invalid evidence categories and
// leaves the profile unpromoted unless every category is current and complete.
// The owner-controlled promotion decision remains separate; this evaluates only
// eligibility.
func EvaluateEligibility(ev *Evidence, candidateProfileID, candidateProfileHash string, includedStages, registeredInvariantKeys []string, now time.Time, maxEvidenceAge time.Duration) Decision {
	dec := Decision{Eligible: false, ProfileID: ev.ProfileID}

	// Mismatched profile: evidence must reference the exact candidate profile.
	if ev.ProfileID != candidateProfileID || ev.ProfileContentHash != candidateProfileHash {
		dec.Rejected = append(dec.Rejected, fmt.Sprintf("mismatched-profile: evidence %s/%s does not match candidate %s/%s", ev.ProfileID, ev.ProfileContentHash, candidateProfileID, candidateProfileHash))
		return dec
	}

	// Aggregate-only: per-stage success/failure evidence is mandatory even when
	// a full aggregate lifecycle is supplied.
	byStage := map[string]StageEvidence{}
	for _, s := range ev.Stages {
		byStage[s.Stage] = s
	}
	for _, stage := range includedStages {
		se, ok := byStage[stage]
		if !ok {
			dec.Missing = append(dec.Missing, fmt.Sprintf("stage %s: per-stage evidence absent", stage))
			continue
		}
		success := RunCurrent(se.Success, now, maxEvidenceAge)
		failure := RunCurrent(se.Failure, now, maxEvidenceAge)
		// Success-path evidence is only valid when the run outcome is 'passed';
		// a current but failed/partial/blocked success run must not count as a
		// passing success path.
		if !success || (se.Success != nil && se.Success.Outcome != "passed") {
			dec.Missing = append(dec.Missing, fmt.Sprintf("stage %s: current success-path evidence", stage))
		}
		if !failure || (se.Failure != nil && !FailurePath(se.Failure.Outcome)) {
			dec.Missing = append(dec.Missing, fmt.Sprintf("stage %s: current failure-path evidence", stage))
		}
	}
	if len(ev.Stages) == 0 && len(includedStages) > 0 {
		dec.Rejected = append(dec.Rejected, "aggregate-only: no per-stage success/failure evidence for included stages")
	}

	// Corrective-Bug coverage: passed, partial, blocked, and failed current
	// evidence bound to exactly one linked corrective Bug.
	corr := ev.Corrective
	if corr.BugRunID == "" {
		dec.Missing = append(dec.Missing, "corrective: no linked corrective Bug")
	}
	if len(corr.DuplicateBugIDs) > 0 {
		dec.Rejected = append(dec.Rejected, fmt.Sprintf("corrective: expected exactly-once bug, found duplicates %v", corr.DuplicateBugIDs))
	}
	if !RunCurrent(corr.Passed, now, maxEvidenceAge) || corr.Passed.Outcome != "passed" {
		dec.Missing = append(dec.Missing, "corrective: current passed evidence")
	}
	if !RunCurrent(corr.Partial, now, maxEvidenceAge) || corr.Partial.Outcome != "partial" {
		dec.Missing = append(dec.Missing, "corrective: current partial evidence")
	}
	if !RunCurrent(corr.Blocked, now, maxEvidenceAge) || corr.Blocked.Outcome != "blocked" {
		dec.Missing = append(dec.Missing, "corrective: current blocked evidence")
	}
	if !RunCurrent(corr.Failed, now, maxEvidenceAge) || corr.Failed.Outcome != "failed" {
		dec.Missing = append(dec.Missing, "corrective: current failed evidence")
	}

	// Gap-ledger proof: every registered invariant needs linked red, green, and
	// a fresh review entry.
	byInvariant := map[string]InvariantEvidence{}
	for _, inv := range ev.Invariants {
		byInvariant[inv.Key] = inv
	}
	for _, key := range registeredInvariantKeys {
		inv, ok := byInvariant[key]
		if !ok {
			dec.Missing = append(dec.Missing, fmt.Sprintf("invariant %s: no gap-ledger row", key))
			continue
		}
		if inv.RedID == "" {
			dec.Missing = append(dec.Missing, fmt.Sprintf("invariant %s: red evidence", key))
		}
		if inv.GreenID == "" {
			dec.Missing = append(dec.Missing, fmt.Sprintf("invariant %s: green evidence", key))
		}
		if inv.ReviewID == "" {
			dec.Missing = append(dec.Missing, fmt.Sprintf("invariant %s: fresh review entry", key))
		}
	}

	// Full aggregate lifecycle: one current intake-to-merge lifecycle that
	// passed aggregate verification and is merge-eligible.
	if ev.Lifecycle == nil {
		dec.Missing = append(dec.Missing, "lifecycle: no full intake-to-merge lifecycle evidence")
	} else if ev.Lifecycle.Superseded {
		dec.Rejected = append(dec.Rejected, "lifecycle: superseded aggregate verification report")
	} else if ev.Lifecycle.RunID == "" || ev.Lifecycle.AggregateID == "" || ev.Lifecycle.CheckpointID == "" || ev.Lifecycle.ReportID == "" || ev.Lifecycle.MergeID == "" {
		dec.Missing = append(dec.Missing, "lifecycle: incomplete intake-to-merge identifiers")
	} else if ev.Lifecycle.Outcome != "passed" {
		dec.Missing = append(dec.Missing, "lifecycle: aggregate verification not passed")
	}

	dec.Eligible = len(dec.Missing) == 0 && len(dec.Rejected) == 0
	return dec
}

// RealPassedLifecycle returns the latest real intake-to-merge aggregate
// lifecycle for a Work Item after it has passed aggregate verification and been
// accepted by the owner (merge-eligible), or nil when no such real lifecycle has
// completed. It derives promotion lifecycle evidence exclusively from immutable
// persisted rows (verification report, checkpoint, owner decision, delivery
// state) so a synthetic or fabricated lifecycle can never satisfy the gate.
func RealPassedLifecycle(db *sql.DB, workItemID string) *LifecycleEvidence {
	var reportID, reportCheckpoint string
	if err := db.QueryRow(`SELECT id,COALESCE(checkpoint_id,'') FROM work_item_verification_reports WHERE work_item_id=? AND status='passed' ORDER BY datetime(created_at) DESC,rowid DESC LIMIT 1`, workItemID).Scan(&reportID, &reportCheckpoint); err != nil {
		return nil
	}
	var acceptID string
	if err := db.QueryRow(`SELECT id FROM work_item_aggregate_owner_decisions WHERE work_item_id=? AND verification_report_id=? AND decision='accepted' ORDER BY datetime(created_at) DESC,rowid DESC LIMIT 1`, workItemID, reportID).Scan(&acceptID); err != nil {
		return nil
	}
	var mergeID, mergeStatus string
	_ = db.QueryRow(`SELECT COALESCE(merged_commit,''),COALESCE(merge_status,'') FROM work_item_delivery_states WHERE work_item_id=?`, workItemID).Scan(&mergeID, &mergeStatus)
	if mergeStatus == "blocked" {
		return nil
	}
	if mergeID == "" {
		mergeID = acceptID // immutable owner-acceptance record id for the merge-eligible state
	}
	runID := reportID // the immutable aggregate-verification report is the intake-to-merge run evidence
	return &LifecycleEvidence{
		RunID: runID, AggregateID: workItemID, CheckpointID: reportCheckpoint,
		ReportID: reportID, MergeID: mergeID, Outcome: "passed",
	}
}

// RegisteredInvariants loads the REQ-* requirement keys that are
// the registered invariants a reusable profile promotion must carry gap-ledger
// proof for, scoped to the candidate Work Item and its requirements.
func RegisteredInvariants(db *sql.DB, workItemID string) []string {
	keys := []string{}
	rows, err := db.Query(`SELECT DISTINCT requirement_key FROM requirements WHERE ((task_id=? AND task_id IS NOT NULL) OR (epic_id=? AND epic_id IS NOT NULL)) AND requirement_key LIKE 'REQ-%' AND status!='deferred' ORDER BY requirement_key`, workItemID, workItemID)
	if err != nil {
		return keys
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

// RejectError renders the rejection as an actionable error listing every
// missing and rejected evidence category. Evidence parsing failure is a hard
// rejection and is returned untouched.
func RejectError(workItemID, profileName string, dec Decision) error {
	reasons := append(append([]string{}, dec.Missing...), dec.Rejected...)
	if len(reasons) == 0 {
		reasons = []string{"promotion evidence is incomplete"}
	}
	return fmt.Errorf("reusable-profile promotion rejected for %s/%s: %s", workItemID, profileName, strings.Join(reasons, "; "))
}

// ProfilePromotionEvaluate is the production promotion entrypoint. It
// resolves the candidate reusable profile, loads the registered invariants, and
// runs the promotion gate over the supplied promotion evidence payload, hard-
// rejecting (returning an error) whenever any required evidence category is
// missing, stale, superseded, mismatched, or otherwise invalid. The mandatory
// intake-to-merge lifecycle is reconciled against real persisted aggregate
// verification state so only a lifecycle that actually passed aggregate
// verification can satisfy the gate. Usage:
//
//	pic workflow profile-promotion-evaluate <work_item_id> <profile_name> [--evidence <json>] [--profile-version <n>]
func ProfilePromotionEvaluate(db *sql.DB, args []string) error {
	if len(args) < 2 {
		return errors.New("profile-promotion-evaluate requires work item id and profile name")
	}
	workItemID, profileName := args[0], args[1]
	if !store.Contains(profile.LifecycleNames, profileName) {
		return fmt.Errorf("invalid reusable profile name %q", profileName)
	}
	if _, err := workitem.ByID(db, workItemID); err != nil {
		return err
	}
	opts, err := store.ParseOptions(args[2:])
	if err != nil {
		return err
	}
	var profileID, profileHash, stagesJSON string
	query := `SELECT id,content_hash,stages_json FROM work_item_profiles WHERE work_item_id=? AND profile_name=? ORDER BY profile_version DESC LIMIT 1`
	queryArgs := []any{workItemID, profileName}
	if opts["profile-version"] != "" {
		if _, err := strconv.Atoi(opts["profile-version"]); err != nil {
			return fmt.Errorf("invalid --profile-version: %q", opts["profile-version"])
		}
		query = `SELECT id,content_hash,stages_json FROM work_item_profiles WHERE work_item_id=? AND profile_name=? AND profile_version=?`
		queryArgs = append(queryArgs, opts["profile-version"])
	}
	if err := db.QueryRow(query, queryArgs...).Scan(&profileID, &profileHash, &stagesJSON); err != nil {
		return fmt.Errorf("no reusable %s profile for Work Item %s: %w", profileName, workItemID, err)
	}
	var includedStages []string
	if err := json.Unmarshal([]byte(stagesJSON), &includedStages); err != nil {
		return fmt.Errorf("corrupt %s profile stages for %s: %w", profileName, workItemID, err)
	}
	registered := RegisteredInvariants(db, workItemID)

	ev, err := ParseEvidence([]byte(opts["evidence"]))
	if err != nil {
		return RejectError(workItemID, profileName, Decision{Rejected: []string{err.Error()}})
	}
	// Reconcile the mandatory intake-to-merge lifecycle against real persisted
	// aggregate verification; a synthetic or fabricated lifecycle never passes.
	ev.Lifecycle = RealPassedLifecycle(db, workItemID)
	dec := EvaluateEligibility(ev, profileID, profileHash, includedStages, registered, time.Now(), 48*time.Hour)
	if !dec.Eligible {
		return RejectError(workItemID, profileName, dec)
	}
	store.WriteJSON(os.Stdout, map[string]any{"eligible": true, "profile_id": profileID, "profile_name": profileName, "work_item_id": workItemID, "reconciled_lifecycle": ev.Lifecycle})
	return nil
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
