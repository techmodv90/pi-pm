import { XMLParser, XMLValidator } from "fast-xml-parser";

interface NamedDescription { name: string; description: string }
interface Pattern { name: string; location: string }
interface Component { name: string; path: string; purpose: string }

export interface CanonicalScanReport {
  techStack: { language: string; framework: string; styling: string; database: string; auth: string; state: string; other: string };
  existingModules: NamedDescription[];
  patternsDetected: Pattern[];
  reusableComponents: Component[];
  gapsDetected: NamedDescription[];
  codeHealth: { typeSafety: string; linting: string; tests: string; debugArtifacts: string; todoFixme: string };
  estimatedSize: { files: string; linesOfCode: string; componentsModules: string; apiRoutesEndpoints: string };
}

const parser = new XMLParser({ trimValues: true, parseTagValue: false });

function object(value: unknown, path: string): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`Canonical Scan Report is missing ${path}`);
  return value as Record<string, unknown>;
}

function text(parent: Record<string, unknown>, key: string, path: string): string {
  const value = parent[key];
  if (typeof value !== "string" || !value.trim()) throw new Error(`Canonical Scan Report is missing ${path}.${key}`);
  return value.trim().replace(/\s+/g, " ");
}

function entries(parent: Record<string, unknown>, containerKey: string, itemKey: string): Record<string, unknown>[] {
  const container = object(parent[containerKey], containerKey);
  const value = container[itemKey];
  const values = Array.isArray(value) ? value : value ? [value] : [];
  return values.map((entry, index) => object(entry, `${containerKey}.${itemKey}[${index}]`));
}

export function parseCanonicalScanReportXml(xml: string): CanonicalScanReport {
  const validation = XMLValidator.validate(xml);
  if (validation !== true) throw new Error(`Canonical Scan Report contains invalid XML: ${validation.err.msg}`);
  const root = object(object(parser.parse(xml), "document").scan_report, "scan_report");
  const tech = object(root.tech_stack || {}, "tech_stack");
  const health = object(root.code_health || {}, "code_health");
  const size = object(root.estimated_size || {}, "estimated_size");
  return {
    techStack: {
      language: text(tech, "language", "tech_stack"), framework: text(tech, "framework", "tech_stack"),
      styling: text(tech, "styling", "tech_stack"), database: text(tech, "database", "tech_stack"),
      auth: text(tech, "auth", "tech_stack"), state: text(tech, "state", "tech_stack"), other: text(tech, "other", "tech_stack"),
    },
    existingModules: entries(root, "existing_modules", "module").map((entry) => ({ name: text(entry, "name", "existing_modules.module"), description: text(entry, "description", "existing_modules.module") })),
    patternsDetected: entries(root, "patterns_detected", "pattern").map((entry) => ({ name: text(entry, "name", "patterns_detected.pattern"), location: text(entry, "location", "patterns_detected.pattern") })),
    reusableComponents: entries(root, "reusable_components", "component").map((entry) => ({ name: text(entry, "name", "reusable_components.component"), path: text(entry, "path", "reusable_components.component"), purpose: text(entry, "purpose", "reusable_components.component") })),
    gapsDetected: entries(root, "gaps_detected", "gap").map((entry) => ({ name: text(entry, "name", "gaps_detected.gap"), description: text(entry, "description", "gaps_detected.gap") })),
    codeHealth: {
      typeSafety: text(health, "type_safety", "code_health"), linting: text(health, "linting", "code_health"), tests: text(health, "tests", "code_health"),
      debugArtifacts: text(health, "debug_artifacts", "code_health"), todoFixme: text(health, "todo_fixme", "code_health"),
    },
    estimatedSize: {
      files: text(size, "files", "estimated_size"), linesOfCode: text(size, "lines_of_code", "estimated_size"),
      componentsModules: text(size, "components_modules", "estimated_size"), apiRoutesEndpoints: text(size, "api_routes_endpoints", "estimated_size"),
    },
  };
}

export function renderScanReportMarkdown(report: CanonicalScanReport): string {
  const lines = [
    "## Scan Report", "", "### TECH_STACK",
    `**Language:** ${report.techStack.language}`, `**Framework:** ${report.techStack.framework}`, `**Styling:** ${report.techStack.styling}`,
    `**Database:** ${report.techStack.database}`, `**Auth:** ${report.techStack.auth}`, `**State:** ${report.techStack.state}`, `**Other:** ${report.techStack.other}`,
    "", "### EXISTING_MODULES", ...report.existingModules.map((item) => `- **${item.name}:** ${item.description}`),
    "", "### PATTERNS_DETECTED", ...report.patternsDetected.map((item) => `- **${item.name}:** ${item.location}`),
    "", "### REUSABLE_COMPONENTS", ...report.reusableComponents.map((item) => `- **${item.name}:** \`${item.path}\` - ${item.purpose}`),
    "", "### GAPS_DETECTED", ...report.gapsDetected.map((item) => `- **${item.name}:** ${item.description}`),
    "", "### CODE_HEALTH", `**Type Safety:** ${report.codeHealth.typeSafety}`, `**Linting:** ${report.codeHealth.linting}`,
    `**Tests:** ${report.codeHealth.tests}`, `**Debug Artifacts:** ${report.codeHealth.debugArtifacts}`, `**TODO/FIXME:** ${report.codeHealth.todoFixme}`,
    "", "### ESTIMATED_SIZE", `**Files:** ${report.estimatedSize.files}`, `**Lines of Code:** ${report.estimatedSize.linesOfCode}`,
    `**Components/Modules:** ${report.estimatedSize.componentsModules}`, `**API Routes/Endpoints:** ${report.estimatedSize.apiRoutesEndpoints}`,
  ];
  return lines.join("\n");
}

export function prepareCanonicalScanReportArtifact(content: string): { content: string; markdown: string } {
  return { content, markdown: renderScanReportMarkdown(parseCanonicalScanReportXml(content)) };
}