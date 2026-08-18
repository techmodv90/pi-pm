package main

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func promotionRun(id, artifact, outcome string, age time.Duration, superseded bool) *promotionRunEvidence {
	ev := &promotionRunEvidence{
		ID:         id,
		ArtifactID: artifact,
		Outcome:    outcome,
		RecordedAt: time.Now().Add(-age).UTC().Format(time.RFC3339),
	}
	if superseded {
		ev.SupersededAt = time.Now().UTC().Format(time.RFC3339)
	}
	return ev
}

func promotionStage(stage string, success, failure *promotionRunEvidence) promotionStageEvidence {
	return promotionStageEvidence{Stage: stage, Success: success, Failure: failure}
}

func promotionCorrective(bugID string, duplicates []string, passed, partial, blocked, failed *promotionRunEvidence) promotionCorrectiveEvidence {
	return promotionCorrectiveEvidence{
		BugRunID: bugID, DuplicateBugIDs: duplicates,
		Passed: passed, Partial: partial, Blocked: blocked, Failed: failed,
	}
}

func promotionInvariant(key, red, green, review string) promotionInvariantEvidence {
	return promotionInvariantEvidence{Key: key, RedID: red, GreenID: green, ReviewID: review}
}

func promotionLifecycle(passed, superseded bool) *promotionLifecycleEvidence {
	outcome := "blocked"
	if passed {
		outcome = "passed"
	}
	return &promotionLifecycleEvidence{
		RunID: "pr-life-001", AggregateID: "wi-agg-001", CheckpointID: "chk-001",
		ReportID: "wivr-001", MergeID: "merge-001", Outcome: outcome, Superseded: superseded,
	}
}

// completePromotionEvidence returns an evidence document that satisfies every
// promotion category for the given candidate profile under the given latency.
func completePromotionEvidence(profileID, hash string, stages, invariants []string) promotionEvidence {
	stagesEv := make([]promotionStageEvidence, 0, len(stages))
	for _, s := range stages {
		stagesEv = append(stagesEv,
			promotionStage(s, promotionRun("pr-"+s+"-pass", "art-"+s+"-pass", "passed", time.Hour, false), promotionRun("pr-"+s+"-fail", "art-"+s+"-fail", "failed", time.Hour, false)))
	}
	invEv := make([]promotionInvariantEvidence, 0, len(invariants))
	for _, k := range invariants {
		invEv = append(invEv, promotionInvariant(k, "red-"+k, "green-"+k, "review-"+k))
	}
	return promotionEvidence{
		ProfileID: profileID, ProfileContentHash: hash,
		Stages: stagesEv,
		Corrective: promotionCorrective("pr-bug-001", nil,
			promotionRun("pr-corr-pass", "art-corr-pass", "passed", time.Hour, false),
			promotionRun("pr-corr-partial", "art-corr-partial", "partial", time.Hour, false),
			promotionRun("pr-corr-blocked", "art-corr-blocked", "blocked", time.Hour, false),
			promotionRun("pr-corr-failed", "art-corr-failed", "failed", time.Hour, false)),
		Invariants: invEv,
		Lifecycle:  promotionLifecycle(true, false),
	}
}

func TestPromotionGate(t *testing.T) {
	now := time.Now()
	maxAge := 48 * time.Hour
	profileID := "wiprof-profile-v3"
	hash := "deadbeef"
	stages := []string{"scan", "rri", "task_graph", "worker", "review"}
	invariants := []string{"REQ-PROMOTION-GATE", "REQ-PIPELINE-PROFILES"}

	t.Run("parse failure is a hard rejection", func(t *testing.T) {
		if _, err := parsePromotionEvidence([]byte("{not json")); err == nil {
			t.Fatal("expected parse failure for malformed payload")
		}
	})

	t.Run("complete current evidence is eligible", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, hash, stages, invariants)
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if !dec.Eligible {
			t.Fatalf("expected eligible, missing=%v rejected=%v", dec.Missing, dec.Rejected)
		}
	})

	t.Run("missing evidence returns all categories and stays unpromoted", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, hash, stages, invariants)
		ev.Lifecycle = nil
		ev.Corrective.Failed = nil
		ev.Stages = ev.Stages[:len(ev.Stages)-1] // drop review stage evidence
		for i := range ev.Invariants {
			ev.Invariants[i].GreenID = ""
		}
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if dec.Eligible {
			t.Fatal("expected not eligible")
		}
		assertMissing := func(substr string) {
			t.Helper()
			for _, m := range dec.Missing {
				if strings.Contains(m, substr) {
					return
				}
			}
			t.Fatalf("missing evidence category %q not reported; got %v", substr, dec.Missing)
		}
		assertMissing("stage review")
		assertMissing("corrective: current failed evidence")
		assertMissing("invariant REQ-PROMOTION-GATE: green evidence")
		assertMissing("invariant REQ-PIPELINE-PROFILES: green evidence")
		assertMissing("lifecycle: no full intake-to-merge lifecycle evidence")
	})

	t.Run("aggregate-only evidence is rejected", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, hash, stages, invariants)
		ev.Stages = nil
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if dec.Eligible {
			t.Fatal("expected aggregate-only evidence rejected")
		}
		found := false
		for _, r := range dec.Rejected {
			if strings.Contains(r, "aggregate-only") {
				found = true
			}
		}
		if !found {
			t.Fatalf("aggregate-only rejection not reported; rejected=%v", dec.Rejected)
		}
	})

	t.Run("mismatched-profile evidence is rejected", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, "wronghash", stages, invariants)
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if dec.Eligible {
			t.Fatal("expected mismatched profile rejected")
		}
		found := false
		for _, r := range dec.Rejected {
			if strings.Contains(r, "mismatched-profile") {
				found = true
			}
		}
		if !found {
			t.Fatalf("mismatched-profile rejection not reported; rejected=%v", dec.Rejected)
		}
	})

	t.Run("stale evidence counts as missing", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, hash, stages, invariants)
		// Supersede every success path and make a failure older than maxAge.
		for i := range ev.Stages {
			ev.Stages[i].Failure.SupersededAt = now.UTC().Format(time.RFC3339)
		}
		ev.Corrective.Passed.RecordedAt = now.Add(-10 * 24 * time.Hour).UTC().Format(time.RFC3339) // older than maxAge
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if dec.Eligible {
			t.Fatal("expected stale evidence rejected")
		}
		found := false
		for _, m := range dec.Missing {
			if strings.Contains(m, "failure-path evidence") {
				found = true
			}
		}
		if !found {
			t.Fatalf("stale failure-path evidence not reported; missing=%v", dec.Missing)
		}
	})

	t.Run("non-passed success run is rejected", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, hash, stages, invariants)
		// A current-but-not-passed success run must not count as success-path
		// evidence: set one included stage's success outcome to 'failed'.
		ev.Stages[0].Success.Outcome = "failed"
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if dec.Eligible {
			t.Fatal("expected a non-passed success run to block eligibility")
		}
		found := false
		want := "stage " + stages[0] + ": current success-path evidence"
		for _, m := range dec.Missing {
			if strings.Contains(m, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("non-passed success run not reported as missing; missing=%v", dec.Missing)
		}
	})

	t.Run("superseded report is rejected", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, hash, stages, invariants)
		ev.Lifecycle = promotionLifecycle(true, true)
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if dec.Eligible {
			t.Fatal("expected superseded report rejected")
		}
		found := false
		for _, r := range dec.Rejected {
			if strings.Contains(r, "superseded") {
				found = true
			}
		}
		if !found {
			t.Fatalf("superseded rejection not reported; rejected=%v", dec.Rejected)
		}
	})

	t.Run("corrective duplicates violate exactly-once", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, hash, stages, invariants)
		ev.Corrective.DuplicateBugIDs = []string{"pr-bug-dup"}
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if dec.Eligible {
			t.Fatal("expected exactly-once violation rejected")
		}
		found := false
		for _, r := range dec.Rejected {
			if strings.Contains(r, "exactly-once") {
				found = true
			}
		}
		if !found {
			t.Fatalf("exactly-once rejection not reported; rejected=%v", dec.Rejected)
		}
	})

	t.Run("incomplete gap-ledger reports missing per invariant", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, hash, stages, invariants)
		ev.Invariants = []promotionInvariantEvidence{promotionInvariant("REQ-PRIVATE", "", "", "")} // unregistered key
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if dec.Eligible {
			t.Fatal("expected not eligible")
		}
		foundRed := false
		for _, m := range dec.Missing {
			if strings.Contains(m, "REQ-PROMOTION-GATE: no gap-ledger row") {
				foundRed = true
			}
		}
		if !foundRed {
			t.Fatalf("unregistered invariant not reported; missing=%v", dec.Missing)
		}
	})

	t.Run("lifecycle not passed blocks eligibility", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, hash, stages, invariants)
		ev.Lifecycle = promotionLifecycle(false, false)
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if dec.Eligible {
			t.Fatal("expected not eligible for unpassed lifecycle")
		}
	})

	t.Run("future-dated evidence is stale", func(t *testing.T) {
		ev := completePromotionEvidence(profileID, hash, stages, invariants)
		ev.Lifecycle.RunID = "pr-life-001"
		ev.Corrective.Blocked.RecordedAt = now.Add(2 * time.Hour).UTC().Format(time.RFC3339) // in the future
		dec := evaluatePromotionEligibility(&ev, profileID, hash, stages, invariants, now, maxAge)
		if dec.Eligible {
			t.Fatal("expected future-dated evidence rejected")
		}
	})
}

// productionPromotionTestDB opens a fresh in-process database, resolves the
// candidate plan profile exactly once, and returns the db, work item id, the
// persisted profile id and content hash, and the profile's included stages.
func productionPromotionTestDB(t *testing.T) (*sql.DB, string, string, string, []string) {
	t.Helper()
	db, id := profileTestDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := ensureWorkItemProfiles(tx, id)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	plan := profiles["plan"]
	var profileID, contentHash string
	if err := db.QueryRow(`SELECT id,content_hash FROM work_item_profiles WHERE work_item_id=? AND profile_name='plan' ORDER BY profile_version DESC LIMIT 1`, id).Scan(&profileID, &contentHash); err != nil {
		t.Fatal(err)
	}
	return db, id, profileID, contentHash, plan.Stages
}

func productionPromotionEvidence(profileID, contentHash string, stages, invariants []string) promotionEvidence {
	return completePromotionEvidence(profileID, contentHash, stages, invariants)
}

// TestPromotionGateProductionEntrypoint exercises the production promotion
// entrypoint so the gate is not dead code: it is wired into the CLI flow and
// reconciles the mandatory intake-to-merge lifecycle against real persisted
// aggregate verification state (never a synthetic fixture).
func TestPromotionGateProductionEntrypoint(t *testing.T) {
	db, workItemID, profileID, contentHash, stages := productionPromotionTestDB(t)
	defer db.Close()

	// The candidate is an epic Work Item; register the matching epics ledger row
	// so requirements can reference it by epic_id.
	if _, err := db.Exec(`INSERT INTO epics(id,title) VALUES(?,?)`, workItemID, "Promotion epic"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"REQ-PROMOTION-GATE", "REQ-PIPELINE-PROFILES"} {
		if _, err := db.Exec(`INSERT INTO requirements(id,epic_id,requirement_key,title,description,acceptance_criteria,priority) VALUES(?,?,?,?,?,?,'tier2')`, "req-"+strings.ToLower(key), workItemID, key, key, "desc", "Given a candidate profile When evaluated Then evidence bound"); err != nil {
			t.Fatal(err)
		}
	}
	invariants := []string{"REQ-PROMOTION-GATE", "REQ-PIPELINE-PROFILES"}
	registerInvariants := func(ev *promotionEvidence) {
		ev.Invariants = []promotionInvariantEvidence{promotionInvariant("REQ-PROMOTION-GATE", "red-1", "green-1", "review-1"), promotionInvariant("REQ-PIPELINE-PROFILES", "red-2", "green-2", "review-2")}
	}
	markComplete := func() promotionEvidence {
		ev := productionPromotionEvidence(profileID, contentHash, stages, invariants)
		registerInvariants(&ev)
		return ev
	}

	// A candidate with complete per-stage, corrective, and gap-ledger evidence
	// is still hard-rejected when no real aggregate lifecycle has passed: the
	// entrypoint must not trust a caller-supplied synthetic lifecycle.
	t.Run("rejects candidate without a real passed aggregate lifecycle", func(t *testing.T) {
		ev := markComplete()
		ev.Lifecycle = promotionLifecycle(true, false) // synthetic — must be ignored
		payload, _ := json.Marshal(ev)
		err := workflowProfilePromotionEvaluate(db, []string{workItemID, "plan", "--evidence", string(payload)})
		if err == nil || !strings.Contains(err.Error(), "lifecycle") {
			t.Fatalf("expected lifecycle rejection without real aggregate verification, got %v", err)
		}
	})

	// Parse failure is a hard rejection in the production flow.
	t.Run("rejects malformed evidence payload", func(t *testing.T) {
		err := workflowProfilePromotionEvaluate(db, []string{workItemID, "plan", "--evidence", "{not json"})
		if err == nil || !strings.Contains(err.Error(), "parse failure") {
			t.Fatalf("expected parse-failure rejection, got %v", err)
		}
	})

	// A candidate whose evidence does not reference the candidate profile is
	// rejected in the production flow.
	t.Run("rejects mismatched-profile evidence", func(t *testing.T) {
		ev := markComplete()
		ev.ProfileID = "wiprof-other"
		payload, _ := json.Marshal(ev)
		err := workflowProfilePromotionEvaluate(db, []string{workItemID, "plan", "--evidence", string(payload)})
		if err == nil || !strings.Contains(err.Error(), "mismatched-profile") {
			t.Fatalf("expected mismatched-profile rejection, got %v", err)
		}
	})

	// Once a real aggregate lifecycle passed aggregate verification and was
	// accepted, and every other category is complete and current, the production
	// flow reconciles the real lifecycle and promotion is eligible.
	t.Run("eligible after a real aggregate lifecycle passed", func(t *testing.T) {
		reportID := "wivr-" + strings.ToLower(profileID) + "-passed"
		if _, err := db.Exec(`INSERT INTO work_item_verification_reports(id,work_item_id,checkpoint_id,status,summary,verified_by_role) VALUES(?,?,?,?,?,?)`, reportID, workItemID, "chk-production-passed", "passed", "aggregate lifecycle passed", "contractor"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO work_item_aggregate_owner_decisions(id,work_item_id,verification_report_id,decision,notes,decided_by_role) VALUES(?,?,?,?,?,?)`, "wiaod-"+reportID, workItemID, reportID, "accepted", "accepted", "owner"); err != nil {
			t.Fatal(err)
		}

		ev := markComplete()
		ev.Lifecycle = nil // no caller-supplied lifecycle; reconciliation supplies it
		payload, _ := json.Marshal(ev)
		if err := workflowProfilePromotionEvaluate(db, []string{workItemID, "plan", "--evidence", string(payload)}); err != nil {
			t.Fatalf("expected eligible after real passed lifecycle, got %v", err)
		}
	})
}
