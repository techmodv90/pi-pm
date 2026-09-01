import { existsSync, lstatSync, readFileSync, readdirSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const SKILL_LIMIT = 1024 * 1024;
const FAMILY_LIMIT = 10 * 1024 * 1024;
const PACKAGE_LIMIT = 50 * 1024 * 1024;

interface ResolveOptions {
  baselineSkills: string[];
  skillFamilies: string[];
  cwd?: string;
  packagedRoot?: string;
  globalRoot?: string;
  projectRoot?: string | null;
}

interface FamilyManifest {
  description: string;
  mandatorySkills: string[];
  appliesTo?: {
    technologies?: string[];
    fileExtensions?: string[];
  };
}

export interface CatalogOptions {
  cwd?: string;
  packagedRoot?: string;
  globalRoot?: string;
  projectRoot?: string | null;
}

export interface SkillFamilyCatalogEntry {
  id: string;
  description: string;
}

function projectSkillsRoot(cwd: string): string | null {
  let current = resolve(cwd);
  while (true) {
    const candidate = join(current, ".pi", "skills");
    if (existsSync(candidate) && statSync(candidate).isDirectory()) return candidate;
    const parent = dirname(current);
    if (parent === current) return null;
    current = parent;
  }
}

function directoryBytes(directory: string): number {
  let total = 0;
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isSymbolicLink()) throw new Error(`skill catalog may not contain symbolic links: ${path}`);
    if (entry.isDirectory()) total += directoryBytes(path);
    else if (entry.isFile()) total += lstatSync(path).size;
  }
  return total;
}

function skillName(directory: string): string {
  const path = join(directory, "SKILL.md");
  const match = readFileSync(path, "utf8").match(/^---\s*\n[\s\S]*?^name:\s*['"]?([^'"\n]+)['"]?\s*$/m);
  if (!match) throw new Error(`skill has no frontmatter name: ${path}`);
  return match[1]!.trim();
}

function skillDirectories(familyDirectory: string): Map<string, string> {
  const found = new Map<string, string>();
  const visit = (directory: string) => {
    if (existsSync(join(directory, "SKILL.md"))) {
      const bytes = directoryBytes(directory);
      if (bytes > SKILL_LIMIT) throw new Error(`skill ${directory} exceeds 1 MiB`);
      found.set(skillName(directory), directory);
      return;
    }
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (entry.isSymbolicLink()) throw new Error(`skill catalog may not contain symbolic links: ${join(directory, entry.name)}`);
      if (entry.isDirectory()) visit(join(directory, entry.name));
    }
  };
  visit(familyDirectory);
  return found;
}

export function resolvedSkillNames(directories: string[]): string[] {
  const names = directories.flatMap((directory) => existsSync(join(directory, "SKILL.md"))
    ? [skillName(directory)]
    : [...skillDirectories(directory).keys()]);
  return [...new Set(names)];
}

function findSkill(root: string, name: string): string | null {
  if (!existsSync(root)) return null;
  const visit = (directory: string): string | null => {
    if (existsSync(join(directory, "SKILL.md")) && skillName(directory) === name) return directory;
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (!entry.isDirectory()) continue;
      const found = visit(join(directory, entry.name));
      if (found) return found;
    }
    return null;
  };
  return visit(root);
}

function readManifest(directory: string): FamilyManifest {
  let value: unknown;
  try { value = JSON.parse(readFileSync(join(directory, "family.json"), "utf8")); }
  catch { throw new Error(`skill family manifest is missing or invalid: ${join(directory, "family.json")}`); }
  const manifest = value as Partial<FamilyManifest>;
  const appliesTo = manifest.appliesTo;
  const validAppliesTo = appliesTo === undefined || (typeof appliesTo === "object" && appliesTo !== null
    && (appliesTo.technologies === undefined || (Array.isArray(appliesTo.technologies) && appliesTo.technologies.every((value) => typeof value === "string")))
    && (appliesTo.fileExtensions === undefined || (Array.isArray(appliesTo.fileExtensions) && appliesTo.fileExtensions.every((value) => typeof value === "string"))));
  if (typeof manifest.description !== "string" || !Array.isArray(manifest.mandatorySkills) || !manifest.mandatorySkills.every((name) => typeof name === "string") || !validAppliesTo) {
    throw new Error(`skill family manifest is invalid: ${join(directory, "family.json")}`);
  }
  return manifest as FamilyManifest;
}

export interface SkillFamilyMatch {
  id: string;
  matchedBy: string[];
}

// Extension tokens (".go") require a trailing boundary so ".go" cannot fire
// inside "foo.gone" or "x.gob"; word tokens keep substring semantics across the
// lowercased evidence text ("SvelteKit" matches "sveltekit").
function evidenceTokenMatches(text: string, token: string): boolean {
  const needle = token.toLowerCase();
  if (!needle.startsWith(".")) return text.includes(needle);
  return new RegExp(`${needle.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}(?![a-z0-9-])`).test(text);
}

export function evaluateSkillFamilyMatches(evidence: unknown, options: CatalogOptions = {}): SkillFamilyMatch[] {
  const text = JSON.stringify(evidence).toLowerCase();
  const manifests = new Map<string, FamilyManifest>();
  const packagedRoot = options.packagedRoot ?? fileURLToPath(new URL("../task-skills", import.meta.url));
  const globalRoot = options.globalRoot ?? join(homedir(), ".pi", "agent", "skills");
  const projectRoot = options.projectRoot === undefined ? projectSkillsRoot(options.cwd || process.cwd()) : options.projectRoot;
  for (const root of [packagedRoot, globalRoot, ...(projectRoot ? [projectRoot] : [])]) {
    if (!existsSync(root)) continue;
    const visit = (directory: string) => {
      if (existsSync(join(directory, "family.json"))) {
        const id = relative(root, directory).split(sep).join("/");
        if (/^[a-z0-9-]+\/[a-z0-9-]+$/.test(id)) manifests.set(id, readManifest(directory));
        return;
      }
      for (const entry of readdirSync(directory, { withFileTypes: true })) if (entry.isDirectory()) visit(join(directory, entry.name));
    };
    visit(root);
  }
  return [...manifests.entries()].map(([id, manifest]) => ({
    id,
    matchedBy: [...(manifest.appliesTo?.technologies || []), ...(manifest.appliesTo?.fileExtensions || [])]
      .filter((token) => evidenceTokenMatches(text, token)),
  })).filter((match) => match.matchedBy.length).sort((a, b) => a.id.localeCompare(b.id));
}

function requireApplicableFamilies(selected: string[], evidence: unknown, options: CatalogOptions, prefix = ""): void {
  const missing = evaluateSkillFamilyMatches(evidence, options).map((match) => match.id).filter((family) => !selected.includes(family));
  if (missing.length) throw new Error(`${prefix}missing applicable skill families: ${missing.join(", ")}`);
}

export function listSkillFamilies(options: CatalogOptions = {}): SkillFamilyCatalogEntry[] {
  const packagedRoot = options.packagedRoot ?? fileURLToPath(new URL("../task-skills", import.meta.url));
  const globalRoot = options.globalRoot ?? join(homedir(), ".pi", "agent", "skills");
  const projectRoot = options.projectRoot === undefined ? projectSkillsRoot(options.cwd || process.cwd()) : options.projectRoot;
  const families = new Map<string, SkillFamilyCatalogEntry>();
  for (const root of [packagedRoot, globalRoot, ...(projectRoot ? [projectRoot] : [])]) {
    if (!existsSync(root)) continue;
    const visit = (directory: string) => {
      if (existsSync(join(directory, "family.json"))) {
        const id = relative(root, directory).split(sep).join("/");
        if (/^[a-z0-9-]+\/[a-z0-9-]+$/.test(id)) families.set(id, { id, description: readManifest(directory).description });
        return;
      }
      for (const entry of readdirSync(directory, { withFileTypes: true })) if (entry.isDirectory()) visit(join(directory, entry.name));
    };
    visit(root);
  }
  return [...families.values()].sort((a, b) => a.id.localeCompare(b.id));
}

export function validateSkillFamilies(skillFamilies: string[], options: CatalogOptions = {}): void {
  const available = listSkillFamilies(options).map(({ id }) => id);
  const valid = new Set(available);
  for (const family of skillFamilies) if (!valid.has(family)) throw new Error(`invalid skill family id: ${family}. Available: ${available.join(", ") || "none"}`);
}

export function validateTaskPlanSkillFamilies(markdown: string, options: CatalogOptions = {}): void {
  const match = markdown.match(/```task-plan-json\s*([\s\S]*?)```/);
  if (!match) return;
  const plan = JSON.parse(match[1]!) as { nodes?: Array<{ key?: unknown; skillFamilies?: unknown }> };
  for (const node of plan.nodes || []) if (Array.isArray(node.skillFamilies) && node.skillFamilies.every((value) => typeof value === "string")) {
    validateSkillFamilies(node.skillFamilies, options);
    requireApplicableFamilies(node.skillFamilies, node, options, typeof node.key === "string" ? `${node.key} ` : "");
  }
}

export function validateInstructionPackSkillFamilies(contentJson: string, options: CatalogOptions = {}, scanEvidence: unknown = []): void {
  const content = JSON.parse(contentJson) as { skillFamilies?: unknown };
  if (Array.isArray(content.skillFamilies) && content.skillFamilies.every((value) => typeof value === "string")) {
    validateSkillFamilies(content.skillFamilies, options);
    requireApplicableFamilies(content.skillFamilies, [content, scanEvidence], options);
  }
}

export function resolveSkillDirectories(options: ResolveOptions): string[] {
  const packagedRoot = options.packagedRoot ?? fileURLToPath(new URL("../task-skills", import.meta.url));
  const globalRoot = options.globalRoot ?? join(homedir(), ".pi", "agent", "skills");
  const projectRoot = options.projectRoot === undefined ? projectSkillsRoot(options.cwd || process.cwd()) : options.projectRoot;
  const packagedRoots = options.packagedRoot || options.globalRoot === undefined ? [packagedRoot] : [];
  const agentSkillsRoot = join(homedir(), ".agents", "skills");
  const trustedRoots = [...packagedRoots, globalRoot, ...(options.globalRoot === undefined ? [agentSkillsRoot] : [])].filter((root) => existsSync(root));
  const baselineRoots = options.globalRoot === undefined ? [...trustedRoots, join(homedir(), ".pi", "agent", "git")] : trustedRoots;
  const resolved: string[] = [];
  let packageBytes = 0;

  validateSkillFamilies(options.skillFamilies, { cwd: options.cwd, packagedRoot, globalRoot, projectRoot });

  for (const name of options.baselineSkills) {
    const directory = baselineRoots.map((root) => findSkill(root, name)).find((candidate): candidate is string => candidate !== null);
    if (!directory) throw new Error(`baseline skill cannot be resolved: ${name}`);
    if (skillName(directory) !== name) throw new Error(`baseline skill name mismatch: ${name}`);
    const bytes = directoryBytes(directory);
    if (bytes > SKILL_LIMIT) throw new Error(`baseline skill ${name} exceeds 1 MiB`);
    packageBytes += bytes;
    resolved.push(directory);
  }

  for (const family of options.skillFamilies) {
    const directories = [...trustedRoots, ...(projectRoot ? [projectRoot] : [])].map((root) => join(root, family)).filter((directory) => existsSync(join(directory, "family.json")));
    if (!directories.length) throw new Error(`skill family cannot be resolved: ${family}`);
    const prefix = `${family.slice(family.lastIndexOf("/") + 1)}-`;
    const merged = new Map<string, string>();
    const mandatory = new Set<string>();
    let familyBytes = 0;
    for (const directory of directories) {
      const manifest = readManifest(directory);
      for (const name of manifest.mandatorySkills) mandatory.add(name);
      for (const [name, skillDirectory] of skillDirectories(directory)) {
        if (!name.startsWith(prefix)) throw new Error(`skill ${name} must use family prefix ${prefix}`);
        merged.set(name, skillDirectory);
      }
      familyBytes += directoryBytes(directory);
    }
    if (familyBytes > FAMILY_LIMIT) throw new Error(`skill family ${family} exceeds 10 MiB`);
    for (const name of mandatory) if (!merged.has(name)) throw new Error(`mandatory skill cannot be resolved in ${family}: ${name}`);
    packageBytes += familyBytes;
    resolved.push(...directories);
  }
  if (packageBytes > PACKAGE_LIMIT) throw new Error("selected skill package exceeds 50 MiB");
  return [...new Set(resolved)];
}