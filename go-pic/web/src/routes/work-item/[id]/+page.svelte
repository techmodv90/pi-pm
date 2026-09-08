<script lang="ts">
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { currentProjectId } from '$lib/stores.js';
  import { api } from '$lib/api.js';
  import type { WorkItem, WorkItemDetail } from '$lib/api.js';

  let id = $state('');
  let projectId = $state('');
  let detail = $state.raw<WorkItemDetail | null>(null);
  let selectedStatus = $state<WorkItem['status']>('open');
  let loading = $state(false);
  let error = $state('');
  let message = $state('');
  let labelInput = $state('');

  page.subscribe((value) => {
    const nextId = value.params.id;
    const nextProject = value.url.searchParams.get('projectId') || '';
    if (nextId !== id || nextProject !== projectId) {
      id = nextId;
      projectId = nextProject;
      if (projectId) { currentProjectId.set(projectId); load(); }
    }
  });

  async function load() {
    loading = true;
    error = '';
    try {
      detail = await api.workItemDetail(projectId, id);
      selectedStatus = detail.workItem.status;
    } catch (cause) { error = cause instanceof Error ? cause.message : 'Failed to load Work Item'; }
    finally { loading = false; }
  }

  async function updateStatus() {
    try {
      await api.updateWorkItemStatus(projectId, id, selectedStatus);
      message = 'Status updated';
      await load();
    } catch (cause) { message = cause instanceof Error ? cause.message : 'Status update failed'; }
  }

  async function addLabels() {
    const labels = labelInput.split(',').map((label) => label.trim()).filter(Boolean);
    if (!labels.length) return;
    try { await api.addWorkItemLabels(projectId, id, labels); labelInput = ''; await load(); }
    catch (cause) { message = cause instanceof Error ? cause.message : 'Label update failed'; }
  }

  async function removeLabel(label: string) {
    try { await api.removeWorkItemLabels(projectId, id, [label]); await load(); }
    catch (cause) { message = cause instanceof Error ? cause.message : 'Label update failed'; }
  }

  function openItem(itemId: string) { goto(resolve('/work-item/[id]', { id: itemId }) + `?projectId=${projectId}`); }
  function value(row: Record<string, unknown>, key: string): string { return row[key] == null ? '' : String(row[key]); }
  function date(raw: string): string { return raw ? new Date(raw.replace(' ', 'T')).toLocaleString() : '—'; }

  // routingEvents payloads arrive as JSON text; parse defensively so a foreign
  // or malformed writer renders as empty instead of breaking the page.
  function routingMissing(raw: unknown): string {
    if (typeof raw !== 'string' || !raw) return '';
    try {
      const parsed = JSON.parse(raw) as Record<string, unknown>;
      return Array.isArray(parsed['missing_families']) && parsed['missing_families'].length
        ? `Missing: ${(parsed['missing_families'] as unknown[]).map(String).join(', ')}`
        : '';
    } catch { return ''; }
  }
</script>

<svelte:head><title>{detail?.workItem.title || 'Work Item'} - pic task system</title></svelte:head>

<button class="btn-link" onclick={() => goto(resolve('/dashboard') + `?projectId=${projectId}`)}>Back to Work Items</button>

{#if loading}
  <div class="loading-list" aria-label="Loading Work Item"><span></span><span></span><span></span></div>
{:else if error}
  <p class="error">{error}</p>
{:else if detail}
  <header class="work-item-header">
    <div><span class="work-item-type">{detail.workItem.type}</span><h2>{detail.workItem.title}</h2><p>{detail.workItem.id}</p></div>
    <div class="header-status"><span class="status-badge {detail.workItem.status}">{detail.workItem.status}</span><span class:ready={detail.ready} class="readiness">{detail.ready ? 'Ready' : 'Blocked'}</span></div>
  </header>

  <div class="meta-grid">
    <div><strong>Priority</strong><span>{detail.workItem.priority}</span></div>
    <div><strong>Parent</strong><span>{detail.workItem.parent_id || 'None'}</span></div>
    <div><strong>Claimed by</strong><span>{detail.workItem.claimed_by || 'Unclaimed'}</span></div>
    <div><strong>Review</strong><span>{detail.workItem.review_status || 'None'}</span></div>
    <div><strong>Deferred</strong><span>{detail.workItem.deferred ? 'Yes' : 'No'}</span></div>
    <div><strong>Created</strong><span>{date(detail.workItem.created_at)}</span></div>
  </div>

  {#if detail.workItem.description}<section class="detail-section"><h3>Description</h3><p class="long-text">{detail.workItem.description}</p></section>{/if}

  <section class="detail-section"><h3>Labels</h3><div class="label-editor"><div class="label-list">{#each detail.workItem.labels as label (label)}<button class="label-chip removable" title={`Remove ${label}`} onclick={() => removeLabel(label)}>{label} x</button>{/each}</div><div class="action-row"><input aria-label="Labels" placeholder="backend,release-v1" bind:value={labelInput} /><button class="btn" onclick={addLabels}>Add</button></div></div></section>

  <section class="detail-section">
    <div class="section-heading"><h3>Hierarchy</h3><span>{detail.descendants.length} descendants</span></div>
    {#if detail.descendants.length === 0}<p class="empty-state compact">No children.</p>{:else}
      <div class="hierarchy-list">{#each detail.descendants as item (item.id)}<button onclick={() => openItem(item.id)} style:--depth={item.depth || 1}><span>{item.title}</span><small>{item.type} · {item.status}</small></button>{/each}</div>
    {/if}
  </section>

  <div class="detail-columns">
    <section class="detail-section"><div class="section-heading"><h3>Dependencies</h3><span>{detail.dependencies.length}</span></div>{#if detail.dependencies.length === 0}<p class="empty-state compact">No blocking dependencies.</p>{:else}<ul class="artifact-list">{#each detail.dependencies as row (value(row, 'depends_on_work_item_id'))}<li><button class="text-button" onclick={() => openItem(value(row, 'depends_on_work_item_id'))}>{value(row, 'title') || value(row, 'depends_on_work_item_id')}</button><span class="status-badge {value(row, 'status')}">{value(row, 'status')}</span></li>{/each}</ul>{/if}</section>
    <section class="detail-section"><div class="section-heading"><h3>Gates</h3><span>{detail.gates.length}</span></div>{#if detail.gates.length === 0}<p class="empty-state compact">No Work Item gates.</p>{:else}<ul class="artifact-list">{#each detail.gates as row (value(row, 'gate_work_item_id'))}<li><button class="text-button" onclick={() => openItem(value(row, 'gate_work_item_id'))}>{value(row, 'title') || value(row, 'gate_work_item_id')}</button><span class="status-badge {value(row, 'status')}">{value(row, 'status')}</span></li>{/each}</ul>{/if}</section>
  </div>

  <section class="detail-section">
    <div class="section-heading"><h3>Artifact Revisions</h3><span>{detail.artifacts.length}</span></div>
    {#if detail.artifacts.length === 0}<p class="empty-state compact">No staged artifacts.</p>{:else}<div class="artifact-table">{#each detail.artifacts as artifact (value(artifact, 'id'))}<div><strong>{value(artifact, 'stage')}</strong><span>Revision {value(artifact, 'revision')}</span><code>{value(artifact, 'content_hash')}</code></div>{/each}</div>{/if}
  </section>

  <div class="detail-columns">
    <section class="detail-section"><div class="section-heading"><h3>Approved Checkpoints</h3><span>{detail.checkpoints.length}</span></div>{#if detail.checkpoints.length === 0}<p class="empty-state compact">No approvals.</p>{:else}<ul class="artifact-list">{#each detail.checkpoints as row (value(row, 'id'))}<li><strong>{value(row, 'stage')} r{value(row, 'artifact_revision')}</strong><code>{value(row, 'content_hash')}</code></li>{/each}</ul>{/if}</section>
    <section class="detail-section"><div class="section-heading"><h3>Implementation Authorization</h3><span>{detail.authorizations.length}</span></div>{#if detail.authorizations.length === 0}<p class="empty-state compact">Not authorized.</p>{:else}<ul class="artifact-list">{#each detail.authorizations as row (value(row, 'id'))}<li><strong>{value(row, 'authorized_by')}</strong><span>{value(row, 'revoked_at') ? 'Revoked' : 'Active'}</span></li>{/each}</ul>{/if}</section>
  </div>

  <section class="detail-section"><div class="section-heading"><h3>Instruction Packs</h3><span>{detail.instructionPacks.length}</span></div>{#if detail.instructionPacks.length === 0}<p class="empty-state compact">No instruction packs.</p>{:else}<div class="artifact-table">{#each detail.instructionPacks as row (value(row, 'id'))}<div><strong>Version {value(row, 'version')}</strong><span class="status-badge {value(row, 'status')}">{value(row, 'status')}</span><code>{value(row, 'content_hash')}</code></div>{/each}</div>{/if}</section>

  <section class="detail-section"><div class="section-heading"><h3>Routing Events</h3><span>{detail.routingEvents.length}</span></div>{#if detail.routingEvents.length === 0}<p class="empty-state compact">No skill routing events recorded.</p>{:else}<ul class="artifact-list">{#each detail.routingEvents as row (value(row, 'createdAt') + value(row, 'summary'))}<li><strong>{date(value(row, 'createdAt'))}</strong><span>{value(row, 'summary')}</span>{#if routingMissing(row.payloadJson)}<small>{routingMissing(row.payloadJson)}</small>{/if}</li>{/each}</ul>{/if}</section>

  <div class="detail-columns">
    <section class="detail-section"><div class="section-heading"><h3>Completion Evidence</h3><span>{detail.completionReports.length}</span></div>{#if detail.completionReports.length === 0}<p class="empty-state compact">No completion reports.</p>{:else}<ul class="artifact-list">{#each detail.completionReports as row (value(row, 'id'))}<li><strong>{value(row, 'status')}</strong><span>{value(row, 'summary')}</span></li>{/each}</ul>{/if}</section>
    <section class="detail-section"><div class="section-heading"><h3>Verification Evidence</h3><span>{detail.verificationReports.length}</span></div>{#if detail.verificationReports.length === 0}<p class="empty-state compact">No verification reports.</p>{:else}<ul class="artifact-list">{#each detail.verificationReports as row (value(row, 'id'))}<li><strong>{value(row, 'status')}</strong><span>{value(row, 'summary')}</span></li>{/each}</ul>{/if}</section>
  </div>

  <section class="detail-section action-section"><h3>Status</h3><div class="action-row"><select bind:value={selectedStatus}><option value="open">Open</option><option value="in_progress">In progress</option><option value="done">Done</option><option value="cancelled">Cancelled</option></select><button class="btn btn-primary" onclick={updateStatus}>Update</button>{#if message}<span class="form-msg">{message}</span>{/if}</div></section>
{/if}