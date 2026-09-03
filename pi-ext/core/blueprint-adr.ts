import { existsSync, mkdirSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";

export interface AdrCandidate {
  context: string;
  choice: string;
  reason: string;
}

// ADR slug constraint: deterministic filesystem-safe slug derived from the
// candidate choice; a choice that slugifies to nothing is rejected fail-closed.
export function adrSlug(choice: string): string {
  const slug = choice.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  if (!slug) throw new Error(`ADR candidate choice produces no safe slug: ${JSON.stringify(choice)}`);
  return slug;
}

// ADR numbering constraint: the next number is max existing NNNN prefix + 1,
// so repeated approvals are deterministic and never overwrite earlier records.
function nextAdrNumber(adrDir: string): number {
  let max = 0;
  if (existsSync(adrDir)) {
    for (const name of readdirSync(adrDir)) {
      const match = /^(\d{4})-/.exec(name);
      if (match) max = Math.max(max, Number(match[1]));
    }
  }
  return max + 1;
}

function adrMarkdown(candidate: AdrCandidate): string {
  return [`# ${candidate.choice}`, "", "**Status**: accepted", "", candidate.context, "", candidate.reason, ""].join("\n");
}

export interface PlannedAdrFile {
  candidate: AdrCandidate;
  filename: string;
  target: string;
}

// OB-F2-3 preflight: full candidate validation (shape, string fields, safe
// slugs) plus target-conflict check and numbering, with no filesystem writes.
// The approval tool runs this BEFORE the Go artifact-save/approve so malformed
// or conflicting input can never leave the canonical Blueprint approved while
// the writer would fail; writeAdrFiles reruns it immediately before writing.
export function planAdrFiles(root: string, candidates: AdrCandidate[]): PlannedAdrFile[] {
  candidates.forEach((candidate, index) => {
    if (!candidate || typeof candidate !== "object" || Array.isArray(candidate)) {
      throw new Error(`adr_candidates[${index}] must be an object with context, choice, and reason`);
    }
    for (const field of ["context", "choice", "reason"] as const) {
      // Type validation is deliberate: no String() coercion, so a malformed
      // v2.1 payload (numbers, null) fails the approval instead of writing a
      // garbage ADR file.
      if (typeof candidate[field] !== "string" || !candidate[field].trim()) {
        throw new Error(`adr_candidates[${index}].${field} must be a non-empty string`);
      }
    }
  });
  const adrDir = join(root, "docs", "adr");
  const start = nextAdrNumber(adrDir);
  const planned = candidates.map((candidate, index) => {
    const filename = `${String(start + index).padStart(4, "0")}-${adrSlug(candidate.choice)}.md`;
    return { candidate, filename, target: join(adrDir, filename) };
  });
  for (const { filename, target } of planned) {
    if (existsSync(target)) throw new Error(`ADR target file already exists: ${filename}`);
  }
  return planned;
}

// OB-F2-3: the only approved docs/adr trigger. Reruns the full validation and
// conflict preflight, so a malformed batch or a file conflict leaves docs/adr
// untouched, then writes exactly one numbered file per candidate.
export function writeAdrFiles(root: string, candidates: AdrCandidate[]): string[] {
  const planned = planAdrFiles(root, candidates);
  const adrDir = join(root, "docs", "adr");
  mkdirSync(adrDir, { recursive: true });
  return planned.map(({ candidate, filename, target }) => {
    writeFileSync(target, adrMarkdown(candidate));
    return join("docs", "adr", filename);
  });
}
