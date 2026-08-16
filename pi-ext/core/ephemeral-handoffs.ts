import { randomUUID } from "node:crypto";

const DEFAULT_TTL_MS = 5 * 60_000;
const DEFAULT_MAX_ENTRY_BYTES = 256 * 1024;
const DEFAULT_MAX_TOTAL_BYTES = 1024 * 1024;

type HandoffEntry = {
  id: string;
  workflow: string;
  workItemId: string;
  payload: string;
  bytes: number;
  expiresAt: number;
};

export class EphemeralHandoffStore {
  private readonly entries = new Map<string, HandoffEntry>();
  private readonly ttlMs: number;
  private readonly maxEntryBytes: number;
  private readonly maxTotalBytes: number;
  private readonly now: () => number;

  constructor(
    ttlMs = DEFAULT_TTL_MS,
    maxEntryBytes = DEFAULT_MAX_ENTRY_BYTES,
    maxTotalBytes = DEFAULT_MAX_TOTAL_BYTES,
    now = () => Date.now(),
  ) {
    this.ttlMs = ttlMs;
    this.maxEntryBytes = maxEntryBytes;
    this.maxTotalBytes = maxTotalBytes;
    this.now = now;
  }

  put(workflow: string, workItemId: string, payload: string): string {
    this.prune();
    const bytes = Buffer.byteLength(payload);
    if (bytes > this.maxEntryBytes) throw new Error(`ephemeral ${workflow} handoff exceeds ${this.maxEntryBytes} bytes`);
    while (this.totalBytes() + bytes > this.maxTotalBytes) {
      const oldest = this.entries.keys().next().value;
      if (!oldest) break;
      this.entries.delete(oldest);
    }
    const id = `handoff-${randomUUID()}`;
    this.entries.set(id, { id, workflow, workItemId, payload, bytes, expiresAt: this.now() + this.ttlMs });
    return id;
  }

  get(id: string, workItemId: string): HandoffEntry | undefined {
    this.prune();
    const entry = this.entries.get(id);
    return entry?.workItemId === workItemId ? entry : undefined;
  }

  delete(id: string): void {
    this.entries.delete(id);
  }

  deleteForWorkItem(workItemId: string, workflow?: string): void {
    for (const [id, entry] of this.entries) {
      if (entry.workItemId === workItemId && (!workflow || entry.workflow === workflow)) this.entries.delete(id);
    }
  }

  clear(): void {
    this.entries.clear();
  }

  private prune(): void {
    const now = this.now();
    for (const [id, entry] of this.entries) if (entry.expiresAt <= now) this.entries.delete(id);
  }

  private totalBytes(): number {
    let total = 0;
    for (const entry of this.entries.values()) total += entry.bytes;
    return total;
  }
}