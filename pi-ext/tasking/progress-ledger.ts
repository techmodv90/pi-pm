// Attempt ledger constraint: a relaunch (retry, review-fix, autofix) receives a
// deterministic evidence ledger built from persisted attempts — prior reports,
// failed verification commands with trimmed output, and escalation resolutions
// (the generalized GAP-138 injection) — so attempt N continues instead of
// re-planning from scratch. The ledger is size-bounded for prompt budgets.

export interface LedgerPriorReport {
  id?: string;
  status?: string;
  summary?: string;
  created_at?: string;
}

export interface LedgerFailedVerification {
  command: string;
  evidence: string;
}

export interface WorkProgressLedgerInput {
  activePackId: string;
  activePackVersion: number | string;
  attempt: number;
  priorReports: LedgerPriorReport[];
  failedVerifications: LedgerFailedVerification[];
  escalationContext: string;
}

const LEDGER_SUMMARY_CHARS = 300;
const LEDGER_EVIDENCE_CHARS = 300;

function boundedDigest(content: string, budget: number): string {
  const flat = String(content || "").trim().replace(/\s+/g, " ");
  return flat.length > budget ? `${flat.slice(0, budget)}…` : flat;
}

function displayTipKey(version: number | string): string {
  return `TIP-${String(version).padStart(3, "0")}`;
}

export function buildWorkProgressLedger(input: WorkProgressLedgerInput): string {
  const lines: string[] = [
    `This is attempt ${input.attempt} of ${displayTipKey(input.activePackVersion)} (pack ${input.activePackId} v${input.activePackVersion}) — continue, do not re-plan from scratch.`,
    "",
    "## Attempt evidence ledger",
  ];
  const reports = input.priorReports.slice(0, 5);
  if (reports.length) {
    for (const report of reports) {
      lines.push(`- ${report.id || "report"} (${report.created_at || "unknown time"}): ${report.status || "unknown"} — ${boundedDigest(report.summary || "no summary", LEDGER_SUMMARY_CHARS)}`);
    }
  } else {
    lines.push("- No prior completion reports for this pack.");
  }
  const verifications = input.failedVerifications.slice(0, 3);
  if (verifications.length) {
    lines.push("", "### Failed verification evidence (from prior attempts)");
    for (const verification of verifications) {
      lines.push(`- \`${verification.command}\`: ${boundedDigest(verification.evidence, LEDGER_EVIDENCE_CHARS)}`);
    }
  }
  if ((input.escalationContext || "").trim()) {
    lines.push("", input.escalationContext.trim());
  }
  return lines.join("\n") + "\n";
}
