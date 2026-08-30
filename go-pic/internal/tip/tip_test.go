package tip

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVerificationGateParsing(t *testing.T) {
	gate, ok := ParseVerificationGate(map[string]any{
		"seam":            "cli-materialize",
		"requirement_keys": []any{"REQ-001"},
		"obligation_keys":  []any{"OB-1", "OB-2"},
		"command":          "go test ./...",
		"expected":         "atomic commit and idempotent repeat",
	})
	if !ok {
		t.Fatal("object gate must parse")
	}
	if gate.Seam != "cli-materialize" || len(gate.RequirementKeys) != 1 || len(gate.ObligationKeys) != 2 || gate.Command != "go test ./..." || gate.Expected != "atomic commit and idempotent repeat" {
		t.Fatalf("gate = %#v", gate)
	}
	if _, ok := ParseVerificationGate("go test ./..."); ok {
		t.Fatal("non-object gate must fail closed")
	}
}

// Decomposition policy v1 artifacts never carry the v2 node fields; the pack
// content they produce must stay byte-compatible, and v2 nodes freeze their
// node-authored acceptance into the pack.
func TestMaterializedPackAcceptanceFreeze(t *testing.T) {
	requirements := map[string]RequirementSnapshot{
		"REQ-1": {RequirementID: "req-1", RequirementKey: "REQ-1", Title: "Required", AcceptanceCriteria: "Given a base\nWhen it runs\nThen it completes"},
	}
	v1Node := TaskPlanDocumentNode{Key: "T01", Goal: "Implement", RequirementKeys: []string{"REQ-1"}}
	content, _, err := MaterializedInstructionPack(v1Node, 3, requirements)
	if err != nil {
		t.Fatal(err)
	}
	var v1Parsed struct {
		Content InstructionPackContent `json:"content"`
	}
	if err := json.Unmarshal(content, &v1Parsed); err != nil {
		t.Fatal(err)
	}
	// A single-requirement node resolves its acceptance from the requirement
	// snapshot so the canonical TIP field carries the effective contract.
	if v1Parsed.Content.Acceptance != requirements["REQ-1"].AcceptanceCriteria {
		t.Fatalf("single-requirement pack acceptance = %q, want the resolved requirement acceptance", v1Parsed.Content.Acceptance)
	}
	// A node composing several requirements without an authored acceptance
	// cannot resolve one; the field stays empty for v1 graphs (v2 rejects it).
	composed := TaskPlanDocumentNode{Key: "T03", Goal: "Compose", RequirementKeys: []string{"REQ-1", "REQ-2"}}
	requirements["REQ-2"] = RequirementSnapshot{RequirementID: "req-2", RequirementKey: "REQ-2", Title: "Second", AcceptanceCriteria: "Given two\nWhen composed\nThen resolved"}
	composedContent, _, err := MaterializedInstructionPack(composed, 3, requirements)
	if err != nil {
		t.Fatal(err)
	}
	var composedParsed struct {
		Content InstructionPackContent `json:"content"`
	}
	if err := json.Unmarshal(composedContent, &composedParsed); err != nil {
		t.Fatal(err)
	}
	if composedParsed.Content.Acceptance != "" {
		t.Fatalf("composed node without authored acceptance must leave the field empty, got %q", composedParsed.Content.Acceptance)
	}
	accepted := "Given an approved graph\nWhen materialization runs\nThen projections commit atomically"
	v2Node := TaskPlanDocumentNode{Key: "T02", Goal: "Compose", RequirementKeys: []string{"REQ-1"}, Acceptance: accepted}
	content, hash, err := MaterializedInstructionPack(v2Node, 3, requirements)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Content InstructionPackContent `json:"content"`
	}
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Content.Acceptance != accepted {
		t.Fatalf("pack acceptance = %q, want the node-authored acceptance", parsed.Content.Acceptance)
	}
	if !strings.Contains(HashJSON(map[string]any{"content": parsed.Content, "requirements": []RequirementSnapshot{requirements["REQ-1"]}}), "sha256:") || hash == "" {
		t.Fatal("content hash must cover the frozen acceptance")
	}
	pack := map[string]any{"id": "wip-x", "version": 1, "status": "active", "content_hash": "sha256:x", "display_key": "TIP-001", "work_item_id": "wi-1", "work_item_title": "Compose", "work_item_type": "task", "priority": "medium", "content_schema_version": 3, "effective_contract_snapshot_id": "", "effective_contract_snapshot_hash": ""}
	expanded := map[string]any{"content_json": string(content)}
	for key, value := range pack {
		expanded[key] = value
	}
	if err := ExpandCanonicalInstructionPack(expanded); err != nil {
		t.Fatal(err)
	}
	rendered := RenderInstructionPack(expanded)
	if !strings.Contains(rendered, "## EFFECTIVE ACCEPTANCE\n"+accepted) {
		t.Fatalf("rendered TIP must freeze the node-authored acceptance:\n%s", rendered)
	}
}
