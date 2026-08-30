// Package tip owns the Task Instruction Pack domain: task-plan parsing,
// instruction pack validation, persistence, activation, and the canonical
// worker-facing render. The pic CLI (package main) remains the only caller
// surface; this package never mutates workflow state beyond the pack row and
// its transactional activation.
package tip

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// newID mirrors the pic CLI short-ID scheme so pack IDs keep their format.
func newID() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strings.ToLower(hex.EncodeToString([]byte(fmt.Sprint("tip"))))[:8]
	}
	return hex.EncodeToString(bytes[:])
}

// contains mirrors the CLI membership helper for plan-node type checks.
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// toInt is a local copy of the CLI coercion helper for pack map rendering.
func toInt(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	default:
		return 0
	}
}

type InstructionPackContent struct {
	Goal                string           `json:"goal"`
	Module              string           `json:"module"`
	EstimatedEffort     int              `json:"estimated_effort_minutes"`
	Files               []string         `json:"files"`
	Patterns            []map[string]any `json:"patterns"`
	BusinessRules       []any            `json:"business_rules"`
	ValidationRules     []any            `json:"validation_rules"`
	ErrorHandling       []any            `json:"error_handling"`
	StateTransitions    []any            `json:"state_transitions"`
	ContractObligations []any            `json:"contract_obligations"`
	Constraints         map[string]any   `json:"constraints"`
	Verification        []any            `json:"verification"`
	SchemaVersion       int              `json:"schemaVersion"`
	SkillFamilies       *[]string        `json:"skillFamilies"`
	ObligationKeys      []string         `json:"obligation_keys"`
	Provides            []string         `json:"provides"`
	Consumes            []string         `json:"consumes"`
	EvidenceFor         []string         `json:"evidence_for"`
	Acceptance          string           `json:"acceptance,omitempty"`
}

// ArtifactLineage binds a downstream artifact to the exact approved predecessor
// artifact revision: id, revision, and content hash.
type ArtifactLineage struct {
	ArtifactID  string `json:"artifact_id"`
	Revision    int    `json:"revision"`
	ContentHash string `json:"content_hash"`
}

type TaskPlanDocument struct {
	Version         int                    `json:"version"`
	ExecutionPolicy string                 `json:"execution_policy"`
	Nodes           []TaskPlanDocumentNode `json:"nodes"`
	// Decomposition policy marker; 0/1 means v1 rules, 2 the vertical-by-default
	// policy with seam-bound verification. Unsupported values fail closed.
	DecompositionPolicyVersion int `json:"decomposition_policy_version,omitempty"`
	// SourceContract binds a policy-v2 graph to the exact approved Contract
	// lineage, mirroring the Contract's source_blueprint binding.
	SourceContract *ArtifactLineage `json:"source_contract,omitempty"`
}

type ContractObligation struct {
	ID              string   `json:"id"`
	RequirementKeys []string `json:"requirement_keys"`
	Behavior        string   `json:"behavior"`
	Acceptance      string   `json:"acceptance"`
	Class           string   `json:"class,omitempty"`
	Seam            string   `json:"seam,omitempty"`
}

type ContractDocument struct {
	ObligationSchemaVersion int                  `json:"obligation_schema_version"`
	Obligations             []ContractObligation `json:"obligations"`
}

// VerificationSeam is one owner-approved place where behavior is proven,
// declared on a decomposition-policy-v2 Blueprint before its approval. The
// highest seam that isolates the requirement under test wins; the aggregate
// itself is always the highest seam.
type VerificationSeam struct {
	ID       string `json:"id"`
	Surface  string `json:"surface"`
	Isolates string `json:"isolates"`
	PriorArt string `json:"prior_art,omitempty"`
}

type TaskPlanDocumentNode struct {
	Key                 string           `json:"key"`
	Type                string           `json:"type"`
	ParentKey           string           `json:"parent_key"`
	Name                string           `json:"name"`
	Goal                string           `json:"goal"`
	RequirementKeys     []string         `json:"requirement_keys"`
	DependsOn           []string         `json:"depends_on"`
	Priority            string           `json:"priority"`
	Module              string           `json:"module"`
	EstimatedEffort     int              `json:"estimated_effort_minutes"`
	Files               []string         `json:"files"`
	Patterns            []map[string]any `json:"patterns"`
	BusinessRules       []any            `json:"business_rules"`
	ValidationRules     []any            `json:"validation_rules"`
	ErrorHandling       []any            `json:"error_handling"`
	StateTransitions    []any            `json:"state_transitions"`
	ContractObligations []any            `json:"contract_obligations"`
	Constraints         map[string]any   `json:"constraints"`
	Verification        []any            `json:"verification"`
	SkillFamilies       *[]string        `json:"skillFamilies"`
	Provides            []string         `json:"provides"`
	Consumes            []string         `json:"consumes"`
	EvidenceFor         []string         `json:"evidence_for"`
	ObligationKeys      []string         `json:"obligation_keys"`
	// Decomposition policy v2 fields. An absent decomposition_mode on a v2
	// document means vertical; v1 documents never carry these fields.
	DecompositionMode  string            `json:"decomposition_mode,omitempty"`
	ExceptionReason    string            `json:"exception_reason,omitempty"`
	PairedContractNode string            `json:"paired_contract_node,omitempty"`
	Acceptance         string            `json:"acceptance,omitempty"`
	DependsOnRationale map[string]string `json:"depends_on_rationale,omitempty"`
}
func ParseTaskPlanJSON(blueprint string) (TaskPlanDocument, error) {
	match := regexp.MustCompile("(?s)```task-plan-json\\s*(.*?)```").FindStringSubmatch(blueprint)
	if len(match) != 2 {
		return TaskPlanDocument{}, errors.New("approved design requires exactly one fenced task-plan-json block")
	}
	if len(regexp.MustCompile("(?s)```task-plan-json\\s*(.*?)```").FindAllStringSubmatch(blueprint, -1)) != 1 {
		return TaskPlanDocument{}, errors.New("approved design requires exactly one fenced task-plan-json block")
	}
	var plan TaskPlanDocument
	if err := json.Unmarshal([]byte(match[1]), &plan); err != nil {
		return plan, fmt.Errorf("task-plan-json: %w", err)
	}
	if (plan.Version < 1 || plan.Version > 3) || len(plan.Nodes) == 0 {
		return plan, errors.New("task-plan-json requires version 1, 2, or 3 and at least one node")
	}
	if plan.ExecutionPolicy == "" {
		plan.ExecutionPolicy = "strict_sequential"
	}
	seen := map[string]bool{}
	nodes := map[string]TaskPlanDocumentNode{}
	for _, node := range plan.Nodes {
		if node.Key == "" || seen[node.Key] {
			return plan, fmt.Errorf("task-plan-json has missing or duplicate node key %q", node.Key)
		}
		seen[node.Key], nodes[node.Key] = true, node
	}
	for _, node := range plan.Nodes {
		kind := node.Type
		if kind == "" {
			kind = "task"
		}
		if !contains([]string{"feature", "task", "bug", "chore", "gate"}, kind) {
			return plan, fmt.Errorf("%s has invalid type %s", node.Key, kind)
		}
		if node.Name == "" {
			return plan, fmt.Errorf("%s requires name", node.Key)
		}
		if node.ParentKey != "" {
			parent, ok := nodes[node.ParentKey]
			parentType := parent.Type
			if parentType == "" {
				parentType = "task"
			}
			if !ok || parentType != "feature" {
				return plan, fmt.Errorf("%s has invalid aggregate parent %s", node.Key, node.ParentKey)
			}
		}
		if kind == "task" || kind == "bug" || kind == "chore" {
			content := InstructionPackContent{Goal: node.Goal, Module: node.Module, EstimatedEffort: node.EstimatedEffort, Files: node.Files, Patterns: node.Patterns, BusinessRules: node.BusinessRules, ValidationRules: node.ValidationRules, ErrorHandling: node.ErrorHandling, StateTransitions: node.StateTransitions, ContractObligations: node.ContractObligations, Constraints: node.Constraints, Verification: node.Verification, SchemaVersion: plan.Version, SkillFamilies: node.SkillFamilies}
			if err := ValidateInstructionPackContent(content); err != nil {
				return plan, fmt.Errorf("%s: %w", node.Key, err)
			}
			if len(node.RequirementKeys) == 0 {
				return plan, fmt.Errorf("%s requires requirement_keys", node.Key)
			}
			if plan.Version >= 2 && node.SkillFamilies == nil {
				return plan, fmt.Errorf("%s requires skillFamilies (use [] when no family applies)", node.Key)
			}
		}
		for _, dependency := range node.DependsOn {
			if !seen[dependency] || dependency == node.Key {
				return plan, fmt.Errorf("%s has invalid dependency %s", node.Key, dependency)
			}
		}
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(key string) error {
		if state[key] == 1 {
			return fmt.Errorf("task-plan-json contains a dependency cycle at %s", key)
		}
		if state[key] == 2 {
			return nil
		}
		state[key] = 1
		for _, dependency := range nodes[key].DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[key] = 2
		return nil
	}
	for key := range nodes {
		if err := visit(key); err != nil {
			return plan, err
		}
	}
	return plan, nil
}
type RequirementSnapshot struct {
	RequirementID      string `json:"requirement_id"`
	RequirementKey     string `json:"requirement_key"`
	Title              string `json:"title"`
	Description        string `json:"description"`
	AcceptanceCriteria string `json:"acceptance_criteria"`
	SourceHash         string `json:"source_hash"`
}
func ActivateInstructionPack(tx *sql.Tx, packID string) error {
	var workItemID string
	if err := tx.QueryRow(`SELECT work_item_id FROM work_item_instruction_packs WHERE id=? AND status='inactive'`, packID).Scan(&workItemID); err != nil {
		return errors.New("instruction pack activation requires an inactive pack")
	}
	if _, err := tx.Exec(`UPDATE pipeline_runs SET status='cancelled',error='instruction pack superseded',updated_at=datetime('now'),completed_at=datetime('now') WHERE task_id=? AND status IN ('claimed','running')`, workItemID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE work_item_instruction_packs SET status='stale',stale_at=datetime('now') WHERE work_item_id=? AND status='active'`, workItemID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE work_item_instruction_packs SET status='active',activated_at=datetime('now') WHERE id=? AND status='inactive'`, packID); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE work_items SET status='open',claimed_at='',claimed_by='',review_status='pending',review_notes='' WHERE id=? AND type IN ('task','bug','chore')`, workItemID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errors.New("instruction pack activation requires an executable Work Item")
	}
	return nil
}
func ExpandCanonicalInstructionPack(pack map[string]any) error {
	var envelope struct {
		Content      InstructionPackContent `json:"content"`
		Requirements []RequirementSnapshot  `json:"requirements"`
	}
	if err := json.Unmarshal([]byte(fmt.Sprint(pack["content_json"])), &envelope); err != nil {
		return fmt.Errorf("instruction pack content: %w", err)
	}
	encode := func(value any) string {
		data, _ := json.Marshal(value)
		return string(data)
	}
	pack["display_key"] = fmt.Sprintf("TIP-%03d", toInt(pack["version"]))
	pack["content_schema_version"] = envelope.Content.SchemaVersion
	pack["files_json"] = encode(envelope.Content.Files)
	pack["patterns_json"] = encode(envelope.Content.Patterns)
	pack["business_rules_json"] = encode(envelope.Content.BusinessRules)
	pack["validation_rules_json"] = encode(envelope.Content.ValidationRules)
	pack["error_handling_json"] = encode(envelope.Content.ErrorHandling)
	pack["state_transitions_json"] = encode(envelope.Content.StateTransitions)
	pack["contract_obligations_json"] = encode(envelope.Content.ContractObligations)
	pack["constraints_json"] = encode(envelope.Content.Constraints)
	pack["verification_json"] = encode(envelope.Content.Verification)
	pack["requirement_snapshots_json"] = encode(envelope.Requirements)
	pack["goal"] = envelope.Content.Goal
	pack["module"] = envelope.Content.Module
	pack["estimated_effort_minutes"] = envelope.Content.EstimatedEffort
	pack["skill_families_json"] = encode(skillFamilies(envelope.Content.SkillFamilies))
	pack["provides_json"] = encode(envelope.Content.Provides)
	pack["consumes_json"] = encode(envelope.Content.Consumes)
	pack["evidence_for_json"] = encode(envelope.Content.EvidenceFor)
	pack["obligation_keys_json"] = encode(envelope.Content.ObligationKeys)
	pack["acceptance"] = envelope.Content.Acceptance
	return nil
}
func ValidateInstructionPackContent(content InstructionPackContent) error {
	missing := []string{}
	if strings.TrimSpace(content.Goal) == "" {
		missing = append(missing, "goal")
	}
	if len(content.Files) == 0 {
		missing = append(missing, "files")
	}
	for name, values := range map[string][]any{"business_rules": content.BusinessRules, "validation_rules": content.ValidationRules, "error_handling": content.ErrorHandling, "state_transitions": content.StateTransitions, "contract_obligations": content.ContractObligations, "verification": content.Verification} {
		if len(values) == 0 {
			missing = append(missing, name)
		}
	}
	if len(content.Constraints) == 0 {
		missing = append(missing, "constraints")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("instruction pack missing required content: %s", strings.Join(missing, ", "))
	}
	if content.SchemaVersion >= 2 {
		if _, err := validateStringList(content.Constraints["generated_files"], "constraints.generated_files"); err != nil {
			return err
		}
		for _, raw := range content.Verification {
			gate, ok := raw.(map[string]any)
			command, commandOK := gate["command"].(string)
			if !ok || !commandOK || strings.TrimSpace(command) == "" {
				return errors.New("instruction pack schema v2 requires an exact verification command for every gate")
			}
			if required, present := gate["required"]; present {
				if _, ok := required.(bool); !ok {
					return errors.New("verification.required must be boolean")
				}
			}
			if _, err := validateStringList(gate["expected_writes"], "verification.expected_writes"); err != nil {
				return err
			}
			requires, err := validateStringList(gate["requires"], "verification.requires")
			if err != nil {
				return err
			}
			setupCommands, err := validateStringList(gate["setup_commands"], "verification.setup_commands")
			if err != nil {
				return err
			}
			if len(requires) > 0 && len(setupCommands) == 0 {
				return errors.New("verification with service prerequisites requires setup_commands")
			}
		}
	}
	return nil
}
func validateStringList(value any, name string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	raw, ok := value.([]any)
	if !ok {
		if values, ok := value.([]string); ok {
			return values, nil
		}
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("%s must contain non-empty strings", name)
		}
		values = append(values, text)
	}
	return values, nil
}
func HashJSON(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func RenderInstructionPack(pack map[string]any) string {
	decode := func(key string, target any) { _ = json.Unmarshal([]byte(fmt.Sprint(pack[key])), target) }
	var files []string
	var patterns []map[string]any
	var businessRules, validation, errorHandling, transitions, obligations, verification []any
	var constraints map[string]any
	var snapshots []RequirementSnapshot
	var families []string
	decode("files_json", &files)
	decode("patterns_json", &patterns)
	decode("business_rules_json", &businessRules)
	decode("validation_rules_json", &validation)
	decode("error_handling_json", &errorHandling)
	decode("state_transitions_json", &transitions)
	decode("contract_obligations_json", &obligations)
	decode("constraints_json", &constraints)
	decode("verification_json", &verification)
	decode("requirement_snapshots_json", &snapshots)
	decode("skill_families_json", &families)
	var provides, consumes, evidenceFor, obligationKeys []string
	decode("provides_json", &provides)
	decode("consumes_json", &consumes)
	decode("evidence_for_json", &evidenceFor)
	decode("obligation_keys_json", &obligationKeys)
	var b strings.Builder
	fmt.Fprintf(&b, "# TASK INSTRUCTION PACK: %v\n\n## HANDOFF VALIDATION\n- Pack: %v\n- Pack version: %v\n- Pack status: %v\n- Pack content hash: %v\n- Result: READY\n\n", pack["display_key"], pack["id"], pack["version"], pack["status"], pack["content_hash"])
	fmt.Fprintf(&b, "## HEADER\n- TIP-ID: %v\n- Pack ID: %v\n- Pack version: %v\n- Content schema version: %v\n- Effective contract snapshot: %s\n- Effective contract hash: %s\n- Work Item: %v\n- Work Item name: %v\n- Work Item type: %v\n- Requirements: %s\n- Module: %v\n- Skill families: %s\n- Depends on: derived from Work Item relations\n- Priority: %v\n\n", pack["display_key"], pack["id"], pack["version"], pack["content_schema_version"], displayOrNone(pack["effective_contract_snapshot_id"]), displayOrNone(pack["effective_contract_snapshot_hash"]), pack["work_item_id"], pack["work_item_title"], pack["work_item_type"], snapshotKeys(snapshots), pack["module"], strings.Join(families, ", "), pack["priority"])
	b.WriteString("## CONTEXT\n- Working directory: current process CWD is authoritative\n- Key files:\n")
	for _, file := range files {
		fmt.Fprintf(&b, "  - `%s`\n", file)
	}
	b.WriteString("- Patterns:\n")
	if len(patterns) == 0 {
		b.WriteString("  - Not applicable: no separate pattern citation approved\n")
	}
	for _, pattern := range patterns {
		fmt.Fprintf(&b, "  - `%v:%v` — %v\n", pattern["file"], pattern["symbol"], pattern["reason"])
	}
	fmt.Fprintf(&b, "\n## TASK\n%v\n\n## SPECIFICATIONS\n\n### Business Rules\n%s\n### Validation\n%s\n### Error Handling\n%s\n### State Transitions\n%s\n### Contract Obligations\n%s\n", pack["goal"], renderItems(businessRules), renderItems(validation), renderItems(errorHandling), renderItems(transitions), renderItems(obligations))
	if len(provides)+len(consumes)+len(evidenceFor)+len(obligationKeys) > 0 {
		fmt.Fprintf(&b, "\n## CONTRACT INTERFACES\n- provides: %s\n- consumes: %s\n- evidence for: %s\n- obligation keys: %s\n", strings.Join(provides, ", "), strings.Join(consumes, ", "), strings.Join(evidenceFor, ", "), strings.Join(obligationKeys, ", "))
	}
	// Effective acceptance freeze: a node-authored acceptance (required when the
	// node composes several requirements) travels into the TIP verbatim; a
	// single-requirement node resolves its acceptance from the snapshot below.
	if acceptance, _ := pack["acceptance"].(string); strings.TrimSpace(acceptance) != "" {
		fmt.Fprintf(&b, "\n## EFFECTIVE ACCEPTANCE\n%s\n", acceptance)
	}
	b.WriteString("## ACCEPTANCE CRITERIA\n")
	for _, snapshot := range snapshots {
		fmt.Fprintf(&b, "\n### %s — %s\n%s\n", snapshot.RequirementKey, snapshot.Title, snapshot.AcceptanceCriteria)
	}
	fmt.Fprintf(&b, "\n## CONSTRAINTS\n%s\n## VERIFICATION\n%s\n## REPORT FORMAT\nReturn the canonical Completion or Issue Report for pack %v version %v and content hash %v.\n", renderObjectItems(constraints), renderItems(verification), pack["id"], pack["version"], pack["content_hash"])
	return b.String()
}
func renderItems(values []any) string {
	var b strings.Builder
	for _, value := range values {
		if text, ok := value.(string); ok {
			fmt.Fprintf(&b, "- %s\n", text)
		} else {
			data, _ := json.Marshal(value)
			fmt.Fprintf(&b, "- `%s`\n", data)
		}
	}
	return b.String()
}
func renderObjectItems(value map[string]any) string { return renderItems([]any{value}) }
func displayOrNone(value any) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return "None"
	}
	return text
}
func snapshotKeys(values []RequirementSnapshot) string {
	keys := make([]string, len(values))
	for i, v := range values {
		keys[i] = v.RequirementKey
	}
	return strings.Join(keys, ", ")
}
func skillFamilies(values *[]string) []string {
	if values == nil {
		return []string{}
	}
	return *values
}
func MaterializedInstructionPack(node TaskPlanDocumentNode, schemaVersion int, requirements map[string]RequirementSnapshot) ([]byte, string, error) {
	// Effective acceptance freeze: a node-authored acceptance travels verbatim;
	// a single-requirement node without one resolves that requirement's
	// acceptance criteria so the TIP carries the effective contract explicitly.
	acceptance := node.Acceptance
	if strings.TrimSpace(acceptance) == "" && len(node.RequirementKeys) == 1 {
		acceptance = requirements[strings.ToUpper(node.RequirementKeys[0])].AcceptanceCriteria
	}
	content := InstructionPackContent{Goal: node.Goal, Module: node.Module, EstimatedEffort: node.EstimatedEffort, Files: node.Files, Patterns: node.Patterns, BusinessRules: node.BusinessRules, ValidationRules: node.ValidationRules, ErrorHandling: node.ErrorHandling, StateTransitions: node.StateTransitions, ContractObligations: node.ContractObligations, Constraints: node.Constraints, Verification: node.Verification, SchemaVersion: schemaVersion, SkillFamilies: node.SkillFamilies, ObligationKeys: node.ObligationKeys, Provides: node.Provides, Consumes: node.Consumes, EvidenceFor: node.EvidenceFor, Acceptance: acceptance}
	snapshots := make([]RequirementSnapshot, 0, len(node.RequirementKeys))
	for _, key := range node.RequirementKeys {
		snapshots = append(snapshots, requirements[strings.ToUpper(key)])
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].RequirementKey < snapshots[j].RequirementKey })
	canonical := map[string]any{"content": content, "requirements": snapshots}
	data, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", err
	}
	return data, HashJSON(canonical), nil
}
// SaveInput carries the parsed content of one instruction-pack save request.
type SaveInput struct {
	ContentJSON        string
	RequirementIDs     []string
	Activate           bool
	ValidateAcceptance func(string) error
}

// SaveInstructionPack resolves the current approved task-graph checkpoint,
// snapshots the bound requirements, and persists one inactive pack inside a
// single transaction, optionally activating it. It returns the persisted row.
func SaveInstructionPack(db *sql.DB, workItemID string, input SaveInput) (map[string]any, error) {
	var content InstructionPackContent
	if err := json.Unmarshal([]byte(input.ContentJSON), &content); err != nil {
		return nil, fmt.Errorf("content-json: %w", err)
	}
	if err := ValidateInstructionPackContent(content); err != nil {
		return nil, err
	}
	if len(input.RequirementIDs) == 0 {
		return nil, errors.New("instruction pack requires at least one requirement")
	}
	snapshots := make([]RequirementSnapshot, 0, len(input.RequirementIDs))
	for _, requirementID := range input.RequirementIDs {
		requirement, err := queryOneMap(db, `SELECT * FROM requirements WHERE id=?`, requirementID)
		if err != nil {
			return nil, fmt.Errorf("Requirement %s not found", requirementID)
		}
		snapshot := RequirementSnapshot{
			RequirementID: requirementID, RequirementKey: fmt.Sprint(requirement["requirement_key"]), Title: fmt.Sprint(requirement["title"]),
			Description: fmt.Sprint(requirement["description"]), AcceptanceCriteria: fmt.Sprint(requirement["acceptance_criteria"]),
		}
		if input.ValidateAcceptance != nil {
			if err := input.ValidateAcceptance(snapshot.AcceptanceCriteria); err != nil {
				return nil, fmt.Errorf("%s acceptance criteria %w", snapshot.RequirementKey, err)
			}
		}
		snapshot.SourceHash = HashJSON(map[string]any{"id": snapshot.RequirementID, "key": snapshot.RequirementKey, "title": snapshot.Title, "description": snapshot.Description, "acceptance_criteria": snapshot.AcceptanceCriteria})
		snapshots = append(snapshots, snapshot)
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].RequirementKey < snapshots[j].RequirementKey })
	canonical := map[string]any{"content": content, "requirements": snapshots}
	contentJSON, err := json.Marshal(canonical)
	if err != nil {
		return nil, err
	}
	contentHash := HashJSON(canonical)
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var checkpointID string
	if err = tx.QueryRow(`SELECT checkpoint_id FROM (
		SELECT id AS checkpoint_id,artifact_revision FROM workflow_checkpoints WHERE work_item_id=? AND stage='task_graph' AND decision_type='approved'
		UNION ALL
		SELECT m.checkpoint_id,c.artifact_revision FROM work_item_materializations m JOIN workflow_checkpoints c ON c.id=m.checkpoint_id WHERE m.work_item_id=? AND c.stage='task_graph'
	) ORDER BY artifact_revision DESC LIMIT 1`, workItemID, workItemID).Scan(&checkpointID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("current task graph is not approved")
		}
		return nil, err
	}
	var version int
	if err = tx.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM work_item_instruction_packs WHERE work_item_id=?`, workItemID).Scan(&version); err != nil {
		return nil, err
	}
	id := "wip-" + newID()
	_, err = tx.Exec(`INSERT INTO work_item_instruction_packs(id,work_item_id,checkpoint_id,version,status,content_json,content_hash) VALUES(?,?,?,?,'inactive',?,?)`, id, workItemID, checkpointID, version, string(contentJSON), contentHash)
	if err != nil {
		return nil, err
	}
	if input.Activate {
		if err = ActivateInstructionPack(tx, id); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return queryOneMap(db, `SELECT * FROM work_item_instruction_packs WHERE id=?`, id)
}

// queryOneMap loads one row as a stringified map, matching the CLI's row shape.
// TaskVerificationGate is the parsed, seam-bound form of one node verification
// entry under decomposition policy v2: it names the seam where behavior is
// proven, at least one requirement or obligation key it evidences, an
// executable command, and the expected evidence statement.
type TaskVerificationGate struct {
	Seam            string
	RequirementKeys []string
	ObligationKeys  []string
	Command         string
	Expected        string
}

// ParseVerificationGate reads one raw node verification entry. ok is false when
// the entry is not a JSON object, so validation can fail closed on malformed
// gates instead of silently skipping them.
func ParseVerificationGate(raw any) (gate TaskVerificationGate, ok bool) {
	entry, ok := raw.(map[string]any)
	if !ok {
		return TaskVerificationGate{}, false
	}
	text := func(key string) string { value, _ := entry[key].(string); return value }
	keys := func(key string) []string {
		values, _ := entry[key].([]any)
		parsed := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				parsed = append(parsed, text)
			}
		}
		return parsed
	}
	return TaskVerificationGate{Seam: text("seam"), RequirementKeys: keys("requirement_keys"), ObligationKeys: keys("obligation_keys"), Command: text("command"), Expected: text("expected")}, true
}

func queryOneMap(db *sql.DB, query string, args ...any) (map[string]any, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("not found")
	}
	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for index := range values {
		pointers[index] = &values[index]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil, err
	}
	row := map[string]any{}
	for index, column := range columns {
		value := values[index]
		if data, ok := value.([]byte); ok {
			value = string(data)
		}
		row[column] = value
	}
	return row, nil
}
