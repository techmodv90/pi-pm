import { randomUUID } from "node:crypto";

const DEFAULT_TTL_MS = 5 * 60_000;
// Delivery of the handoff notification can lag behind a busy session, so an
// entry that was never loaded must outlive the read TTL or the contractor can
// never retrieve evidence that was announced to it.
const DEFAULT_UNREAD_TTL_MS = 30 * 60_000;
const DEFAULT_MAX_ENTRY_BYTES = 256 * 1024;
const DEFAULT_MAX_TOTAL_BYTES = 1024 * 1024;

type HandoffEntry = {
  id: string;
  workflow: string;
  workItemId: string;
  payload: string;
  bytes: number;
  expiresAt: number;
  loadedAt?: number;
};

export class EphemeralHandoffStore {
  private readonly entries = new Map<string, HandoffEntry>();
  private readonly ttlMs: number;
  private readonly unreadTtlMs: number;
  private readonly maxEntryBytes: number;
  private readonly maxTotalBytes: number;
  private readonly now: () => number;

  constructor(
    ttlMs = DEFAULT_TTL_MS,
    maxEntryBytes = DEFAULT_MAX_ENTRY_BYTES,
    maxTotalBytes = DEFAULT_MAX_TOTAL_BYTES,
    now = () => Date.now(),
    unreadTtlMs = DEFAULT_UNREAD_TTL_MS,
  ) {
    this.ttlMs = ttlMs;
    this.unreadTtlMs = unreadTtlMs;
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
    this.entries.set(id, { id, workflow, workItemId, payload, bytes, expiresAt: this.now() + this.unreadTtlMs });
    return id;
  }

  get(id: string, workItemId: string): HandoffEntry | undefined {
    this.prune();
    const entry = this.entries.get(id);
    if (!entry || entry.workItemId !== workItemId) return undefined;
    // First load starts the read window: the payload is now in the consuming
    // conversation, so ephemerality only has to cover the post-read lifetime.
    if (!entry.loadedAt) {
      entry.loadedAt = this.now();
      entry.expiresAt = this.now() + this.ttlMs;
    }
    return entry;
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