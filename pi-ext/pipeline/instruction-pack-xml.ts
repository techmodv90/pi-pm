
export function markdownLabel(key: string): string {
  const label = key.replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/[_-]+/g, " ").toLowerCase();
  return label.charAt(0).toUpperCase() + label.slice(1);
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function markdownObject(value: Record<string, any>, indent: string, nested: boolean): string {
  return Object.entries(value).map(([key, field], index) => {
    const fieldIndent = nested && index > 0 ? `${indent}  ` : indent;
    if (field && typeof field === "object" && !Array.isArray(field)) {
      return `${fieldIndent}- **${markdownLabel(key)}:**\n${markdownObject(field, `${fieldIndent}  `, false)}`;
    }
    if (Array.isArray(field) && field.some((item) => item && typeof item === "object")) {
      return `${fieldIndent}- **${markdownLabel(key)}:**\n${markdownItems(field, `${fieldIndent}  `)}`;
    }
    const text = Array.isArray(field) ? field.join(", ") || "None" : field == null || field === "" ? "None" : String(field);
    return `${fieldIndent}- **${markdownLabel(key)}:** ${text}`;
  }).join("\n") || `${indent}- None`;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function markdownItems(values: any, indent = ""): string {
  if (values && typeof values === "object" && !Array.isArray(values)) return markdownObject(values, indent, false);
  return (Array.isArray(values) ? values : values == null ? [] : [values])
    .map((value) => value && typeof value === "object" ? markdownObject(value, indent, true) : `${indent}- ${value}`)
    .join("\n") || `${indent}- None`;
}

export function xmlEscape(value: unknown): string {
  return String(value ?? "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&apos;");
}

export function xmlValue(tag: string, value: unknown): string {
  return `<${tag}>${xmlEscape(value)}</${tag}>`;
}

export function xmlFields(value: unknown): string {
  if (!value || typeof value !== "object" || Array.isArray(value)) return xmlValue("value", value);
  return Object.entries(value).map(([key, field]) => {
    const tag = key.replace(/[^a-zA-Z0-9_.-]/g, "_");
    if (Array.isArray(field)) return `<${tag}>${field.map((entry) => xmlFields(entry)).join("")}</${tag}>`;
    if (field && typeof field === "object") return `<${tag}>${xmlFields(field)}</${tag}>`;
    return xmlValue(tag, field);
  }).join("");
}

export function xmlCollection(tag: string, itemTag: string, value: unknown): string {
  const values = Array.isArray(value) ? value : value == null ? [] : [value];
  return `<${tag}>${values.map((entry) => `<${itemTag}>${entry && typeof entry === "object" ? xmlFields(entry) : xmlEscape(entry)}</${itemTag}>`).join("")}</${tag}>`;
}

export function validateInstructionPackXml(output: string): void {
  const root = output.trim().match(/^<instruction_pack\s+schema_version="1"\s+display_key="[^"]+"\s+id="[^"]+"\s+version="[^"]+"\s+content_hash="[^"]+">([\s\S]*)<\/instruction_pack>$/);
  if (!root) throw new Error("Worker handoff must be one versioned <instruction_pack> XML document");
  for (const element of ["pipeline_ownership", "handoff_validation", "header", "context", "task", "specifications", "requirements", "constraints", "verification", "report_format"]) {
    if (!root[1].includes(`<${element}>`) || !root[1].includes(`</${element}>`)) throw new Error(`Instruction Pack XML missing <${element}>`);
  }
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
export function renderCanonicalInstructionPackXml(item: any, pack: any): string {
  const envelope = JSON.parse(pack.content_json || "{}");
  const content = envelope.content || envelope;
  const requirements = Array.isArray(envelope.requirements)
    ? envelope.requirements
    : Array.isArray(content.requirement_snapshots) ? content.requirement_snapshots : [];
  const files = (content.files || []).map((file: unknown) => xmlValue("file", file)).join("");
  const patterns = (content.patterns || []).map((pattern: unknown) => `<pattern>${xmlFields(pattern)}</pattern>`).join("");
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- legacy baseline (pre-split scheduler)
  const requirementXml = requirements.map((requirement: any) => `<requirement key="${xmlEscape(requirement.requirement_key || requirement.requirement_id || "Requirement")}">${xmlValue("title", requirement.title || "Acceptance")}${xmlValue("acceptance_criteria", requirement.acceptance_criteria || "")}</requirement>`).join("");
  const output = `<instruction_pack schema_version="1" display_key="${xmlEscape(pack.display_key || `TIP-${pack.version}`)}" id="${xmlEscape(pack.id)}" version="${xmlEscape(pack.version)}" content_hash="${xmlEscape(pack.content_hash)}">
  <pipeline_ownership>
    <instruction>The scheduler has already claimed and launched this Work Item. Implement the bounded scope directly.</instruction>
    <instruction>Do not call work_on_work_item, pipeline-claim, reset_pipeline_circuit, or other lifecycle-control actions from this worker.</instruction>
  </pipeline_ownership>
  <handoff_validation>${xmlValue("status", "READY")}${xmlValue("pack_status", pack.status)}${xmlValue("content_hash", pack.content_hash)}</handoff_validation>
  <header>${xmlValue("work_item", item.id)}${xmlValue("work_item_type", item.type)}${xmlValue("priority", item.priority || "medium")}${xmlCollection("skill_families", "skill_family", content.skill_families || content.skillFamilies)}</header>
  <context><working_directory>current process CWD is authoritative</working_directory><files>${files}</files><patterns>${patterns}</patterns></context>
  ${xmlValue("task", content.goal || item.description || item.title)}
  <specifications>${xmlCollection("business_rules", "rule", content.business_rules)}${xmlCollection("validation_rules", "rule", content.validation_rules)}${xmlCollection("error_handling", "rule", content.error_handling)}${xmlCollection("state_transitions", "transition", content.state_transitions)}${xmlCollection("contract_obligations", "obligation", content.contract_obligations)}</specifications>
  ${(content.provides || content.consumes || content.evidence_for) ? `<contract_interfaces>${xmlCollection("provides", "obligation", content.provides)}${xmlCollection("consumes", "obligation", content.consumes)}${xmlCollection("evidence_for", "obligation", content.evidence_for)}</contract_interfaces>` : ""}
  <requirements>${requirementXml}</requirements>
  <constraints>${xmlFields(content.constraints || {})}</constraints>
  ${xmlCollection("verification", "check", content.verification)}
  <report_format>Return the canonical Completion or Issue Report for pack ${xmlEscape(pack.id)} version ${xmlEscape(pack.version)} and content hash ${xmlEscape(pack.content_hash)}.</report_format>
</instruction_pack>`;
  validateInstructionPackXml(output);
  return output;
}
