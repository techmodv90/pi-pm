package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// Verification-only graph rule: a graph whose nodes are all gate nodes
// (retrospective aggregates) must evidence every Contract obligation at a
// gate node; the executable provider rule does not apply.
func TestVerificationOnlyGraphEvidence(t *testing.T) {
	bin := buildPic(t)
	root, home := initProject(t, bin)
	epic := asObject(t, runPic(t, bin, root, home, "work-item", "create", "epic", "Retrospective Epic"))
	id := epic["id"].(string)
	dbPath := filepath.Join(root, ".pi", "tasks.db")
	seedV2Requirements(t, dbPath, id)
	blueprint := approveV2Blueprint(t, bin, root, home, id)
	contractContent := v2ContractArtifact(blueprint["id"].(string), int(blueprint["revision"].(float64)), blueprint["content_hash"].(string))
	contract := asObject(t, runPic(t, bin, root, home, "work-item", "artifact-save", id, "contracts", contractContent))
	runPic(t, bin, root, home, "work-item", "artifact-approve", id, "contracts", contract["id"].(string), "approved")

	graphJSON := verificationOnlyTaskGraph(contract["id"].(string), int(contract["revision"].(float64)), contract["content_hash"].(string))
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", graphJSON)
	validated := asObject(t, runPic(t, bin, root, home, "work-item", "graph-validate", id))
	if validated["valid"] != true {
		t.Fatalf("verification-only graph validation = %#v", validated)
	}

	// An unevidenced obligation must fail closed.
	bad := strings.Replace(graphJSON, `"provides":[],"consumes":[],"evidence_for":["OB-001"],"obligation_keys":["OB-001"]`, `"provides":[],"consumes":[],"evidence_for":[],"obligation_keys":[]`, 1)
	runPic(t, bin, root, home, "work-item", "artifact-save", id, "task_graph", bad)
	if out := runPicError(t, bin, root, home, "work-item", "graph-validate", id); !strings.Contains(out, "verification-only Task Graph must evidence Contract obligation OB-001") {
		t.Fatalf("unevidenced obligation error = %s", out)
	}
}

// verificationOnlyTaskGraph builds an all-gate policy-v2 graph: the
// retrospective-aggregate shape where every obligation is evidenced at a
// verification gate instead of provided by an executable node.
func verificationOnlyTaskGraph(contractArtifactID string, contractRevision int, contractContentHash string) string {
	return fmt.Sprintf(`{"version":3,"execution_policy":"parallel_allowed","decomposition_policy_version":2,"source_contract":{"artifact_id":%q,"revision":%d,"content_hash":%q},"nodes":[`+
		`{"key":"G01","type":"gate","name":"Gate one","goal":"Verify delivered behavior","requirement_keys":["REQ-001"],"depends_on":[],"depends_on_rationale":{},"decomposition_mode":"integration_gate","exception_reason":"verifies delivered features at the highest seam","priority":"P1","module":"verification-only integration gate","skillFamilies":[],"estimated_effort_minutes":30,"files":["x_test.go"],"patterns":[],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["go-pic/cmd/pic"]},"verification":[{"seam":"cli-materialize","requirement_keys":["REQ-001"],"obligation_keys":["OB-001"],"command":"go test ./...","expected":"delivered behavior verified","required":true,"requires":[],"expected_writes":[],"setup_commands":[]}],"provides":[],"consumes":[],"evidence_for":["OB-001"],"obligation_keys":["OB-001"]},`+
		`{"key":"G02","type":"gate","name":"Gate two","goal":"Verify cross-obligation integration","requirement_keys":["REQ-002"],"depends_on":["G01"],"depends_on_rationale":{"G01":"grades both obligations together"},"decomposition_mode":"integration_gate","exception_reason":"verifies delivered features at the highest seam","priority":"P1","module":"verification-only integration gate","skillFamilies":[],"estimated_effort_minutes":30,"files":["x_test.go"],"patterns":[],"business_rules":["rule"],"validation_rules":["rule"],"error_handling":["rule"],"state_transitions":["rule"],"contract_obligations":["rule"],"constraints":{"scope_roots":["go-pic/cmd/pic"]},"verification":[{"seam":"cli-materialize","requirement_keys":["REQ-002"],"obligation_keys":["OB-002"],"command":"go test ./...","expected":"delivered behavior verified","required":true,"requires":[],"expected_writes":[],"setup_commands":[]}],"provides":[],"consumes":[],"evidence_for":["OB-002","OB-003"],"obligation_keys":["OB-002","OB-003"]}`+
		`]}`, contractArtifactID, contractRevision, contractContentHash)
}
