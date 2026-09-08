{
  "personas": ["End User", "Operator", "Developer"],
  "scenarios": [
    {
      "id": "SC-1",
      "persona": "End User",
      "dimension": "D1",
      "stress_axis": "DATA",
      "requirement_id": "R04",
      "procedure": "Run `cd go-pic && go test ./cmd/pic -run TestArtifactRevisionCreatesNewFile -count=1` on feat/p2-important @ 8aff156; assert revision 2 writes blueprint-r2.md at a new deterministic path while revision 1 bytes remain byte-identical.",
      "remediation_hint": "",
      "evidence": "Exit 0 on feat/p2-important @ 8aff156 (aggregate acceptance run 2026-09-08, /tmp/pi-agent-f2review); T011 test written as regression coverage after T009 delivered the revision projection (recorded deviation, owner-waivered).",
      "result": "PASS"
    },
    {
      "id": "SC-2",
      "persona": "Operator",
      "dimension": "D1",
      "stress_axis": "ERROR",
      "requirement_id": "R05",
      "procedure": "Run `cd go-pic && go test ./cmd/pic -run TestArtifactFileConflictBlocksSave -count=1` on feat/p2-important @ 8aff156; assert a save over pre-created divergent contracts-r1.md bytes fails with 'artifact file conflict' + path, leaves bytes unchanged, and inserts no artifact row.",
      "remediation_hint": "",
      "evidence": "Exit 0 on feat/p2-important @ 8aff156; conflict pre-flight lives at go-pic/cmd/pic/work_items.go:1719 (T009), T012 test is regression coverage (recorded deviation, owner-waivered).",
      "result": "PASS"
    },
    {
      "id": "SC-3",
      "persona": "Developer",
      "dimension": "D1",
      "stress_axis": "SCALE",
      "requirement_id": "R06",
      "procedure": "Run `cd go-pic && go test ./cmd/pic -run TestArtifactFileIntegrityCheck -count=1` on feat/p2-important @ 8aff156; assert `pic work-item artifact-check` reports ok/drift/missing per bound artifact with every entry carrying non-empty artifact_id and file_path plus status, on-demand sha256 (hashJSON) comparison per NC-7.",
      "remediation_hint": "",
      "evidence": "Exit 0 on feat/p2-important @ 8aff156; implemented in T014 (workItemArtifactCheck, artifact_files.go +45), review pr-3d9f5af1 passed; NC-7 shape gate asserted in TestArtifactFileIntegrityCheck (T013 fix round).",
      "result": "PASS"
    },
    {
      "id": "SC-4",
      "persona": "Operator",
      "dimension": "D2",
      "stress_axis": "ERROR",
      "requirement_id": "R11",
      "procedure": "Confirm no existing file is mutated when a projection or save fails: run `cd go-pic && go test ./cmd/pic -run 'TestArtifactProjectionFailureIsBestEffort|TestArtifactFileConflictBlocksSave' -count=1` on feat/p2-important @ 8aff156.",
      "remediation_hint": "",
      "evidence": "Exit 0 (TestArtifactProjectionFailureIsBestEffort verified at F1 aggregate wivr-d1ad7f10; TestArtifactFileConflictBlocksSave verified here — unchanged-bytes assertion is R11's no-mutation guarantee on the save path).",
      "result": "PASS"
    }
  ],
  "not_applicable": [
    {"id": "NA-1", "requirement_id": "R07", "reason": "Owned by F3: T015 (RED backfill test) and T016 (GREEN backfill) are open; T014 deliberately did not route artifact-backfill (reviewer-validated deviation to avoid pre-satisfying T015's RED phase)."},
    {"id": "NA-2", "requirement_id": "R08", "reason": "Owned by F3: T017/T018 (detail payload and dashboard file_path display) are open."},
    {"id": "NA-3", "requirement_id": "R01", "reason": "Verified at the F1 aggregate (wivr-d1ad7f10, all scenarios PASS on feat/p1-critical-path); no F2 delta touches the save projection path."},
    {"id": "NA-4", "requirement_id": "R02", "reason": "Verified at the F1 aggregate (wivr-d1ad7f10); no F2 delta touches projection failure handling."},
    {"id": "NA-5", "requirement_id": "R03", "reason": "Verified at the F1 aggregate (wivr-d1ad7f10); no F2 delta touches planning-stage projection."},
    {"id": "NA-6", "requirement_id": "R09", "reason": "Verified at the F1 aggregate (wivr-d1ad7f10); artifact_files schema untouched by F2."},
    {"id": "NA-7", "requirement_id": "R10", "reason": "Verified at the F1 aggregate (p95 7.199ms vs 50ms limit, wivr-d1ad7f10); F2 adds no projection-path code."},
    {"id": "NA-8", "requirement_id": "R11", "reason": "Graded PASS as SC-4; no residual F2-owned portion remains unverified (T019 dashboard-side work is F3)."}
  ],
  "open_blockers": []
}