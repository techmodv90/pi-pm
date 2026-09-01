<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { resolve } from '$app/paths';
  import { page } from '$app/stores';
  import { currentProjectId, workItems, activeTab } from '$lib/stores.js';
  import { api } from '$lib/api.js';
  import type { ProjectSummary, WorkItem, WorkItemDetail, WorkItemLabel, WorkflowItem, WorkflowAnalyticsRow, SkillRoutingStats } from '$lib/api.js';

  let pid = $state<string | null>(null);
  let summary = $state<ProjectSummary | null>(null);
  let items = $state.raw<WorkItem[]>([]);
  let readyIds = $state.raw<Set<string>>(new Set());
  let queues = $state.raw<Record<string, WorkflowItem[]>>({ review: [], verification: [], escalations: [], blocked: [] });
  let analytics = $state.raw<WorkflowAnalyticsRow[]>([]);
  let routing = $state.raw<SkillRoutingStats | null>(null);
  let labels = $state.raw<WorkItemLabel[]>([]);
  let selectedId = $state('');
  let selectedDetail = $state.raw<WorkItemDetail | null>(null);
  let detailLoading = $state(false);
  let tab = $state('work-items');
  let loading = $state(false);
  let error = $state('');
  let showCreate = $state(false);
  let message = $state('');
  let filters = $state({ type: '', status: '', readiness: 'ready', label: '', q: '' });
  let draft = $state({ type: 'task' as WorkItem['type'], title: '', description: '', priority: 'medium' as WorkItem['priority'], parent_id: '', labels: '' });

  const aggregateParents = $derived(items.filter((item) => item.type === 'epic' || item.type === 'feature'));
  const filteredItems = $derived(items.filter((item) => {
    if (filters.type && item.type !== filters.type) return false;
    if (filters.status && item.status !== filters.status) return false;
    if (filters.readiness === 'ready' && !readyIds.has(item.id)) return false;
    if (filters.readiness === 'blocked' && readyIds.has(item.id)) return false;
    if (filters.label && !item.labels.includes(filters.label)) return false;
    const query = filters.q.trim().toLowerCase();
    return !query || item.title.toLowerCase().includes(query) || item.description.toLowerCase().includes(query);
  }));

  onMount(() => {
    const unsubscribePage = page.subscribe((value) => {
      const projectId = value.url.searchParams.get('projectId');
      if (projectId) currentProjectId.set(projectId);
    });
    const unsubscribeProject = currentProjectId.subscribe((projectId) => {
      if (projectId && projectId !== pid) {
        pid = projectId;
        load(projectId);
      }
    });
    const unsubscribeItems = workItems.subscribe((value) => items = value);
    const unsubscribeTab = activeTab.subscribe((value) => tab = value);
    return () => { unsubscribePage(); unsubscribeProject(); unsubscribeItems(); unsubscribeTab(); };
  });

  async function load(projectId: string) {
    loading = true;
    error = '';
    try {
      const [summaryData, itemData, labelData, readyData, review, verification, escalations, blocked] = await Promise.all([
        api.projectSummary(projectId), api.workItems(projectId), api.workItemLabels(projectId), api.readyWorkItems(projectId),
        api.workflowQueue('review-queue').catch(() => ({ items: [] })),
        api.workflowQueue('verification-queue').catch(() => ({ items: [] })),
        api.workflowQueue('escalations').catch(() => ({ items: [] })),
        api.workflowQueue('blocked').catch(() => ({ items: [] })),
      ]);
      summary = summaryData;
      workItems.set(itemData.workItems);
      labels = labelData.labels;
      readyIds = new Set(readyData.workItems.map((item) => item.id));
      const firstReady = itemData.workItems.find((item) => readyIds.has(item.id));
      if (firstReady) await selectItem(firstReady.id, false);
      queues = {
        review: review.items.filter((item) => item.projectId === projectId),
        verification: verification.items.filter((item) => item.projectId === projectId),
        escalations: escalations.items.filter((item) => item.projectId === projectId),
        blocked: blocked.items.filter((item) => item.projectId === projectId),
      };
    } catch (cause) {
      error = cause instanceof Error ? cause.message : 'Failed to load project';
    } finally {
      loading = false;
    }
  }

  async function switchTab(next: string) {
    tab = next;
    activeTab.set(next);
    if (next === 'analytics' && pid) {
      try { analytics = (await api.workflowAnalytics(pid)).rows; } catch { analytics = []; }
    }
    if (next === 'routing' && pid) {
      try { routing = await api.skillRouting(pid); } catch { routing = null; }
    }
  }

  // The skill-routing endpoint returns JSON array columns as JSON text; parse
  // defensively so a foreign or malformed writer renders as "—" instead of
  // breaking the tab.
  function jsonList(value: unknown): string[] {
    if (typeof value !== 'string' || !value) return [];
    try {
      const parsed = JSON.parse(value);
      return Array.isArray(parsed) ? parsed.map((entry) => String(entry)) : [];
    } catch { return []; }
  }

  function tokenList(value: unknown): string {
    const tokens = jsonList(value);
    return tokens.length ? tokens.join(', ') : '—';
  }

  function familyMatches(value: unknown): string {
    if (typeof value !== 'string' || !value) return '—';
    try {
      const parsed = JSON.parse(value) as Array<Record<string, unknown>>;
      return parsed.length ? parsed.map((entry) => String(entry?.id ?? '')).join(', ') : '—';
    } catch { return '—'; }
  }

  async function createItem() {
    if (!pid || !draft.title.trim()) { message = 'Title is required'; return; }
    try {
      const itemLabels = draft.labels.split(',').map((label) => label.trim()).filter(Boolean);
      await api.createWorkItem(pid, { ...draft, labels: itemLabels, title: draft.title.trim(), description: draft.description.trim(), parent_id: draft.parent_id || undefined });
      draft = { type: 'task', title: '', description: '', priority: 'medium', parent_id: '', labels: '' };
      showCreate = false;
      message = '';
      await load(pid);
    } catch (cause) { message = cause instanceof Error ? cause.message : 'Create failed'; }
  }

  async function selectItem(id: string, useResponsiveNavigation = true) {
    if (!pid) return;
    if (useResponsiveNavigation && window.matchMedia('(max-width: 899px)').matches) {
      goto(resolve('/work-item/[id]', { id }) + `?projectId=${pid}`);
      return;
    }
    selectedId = id;
    detailLoading = true;
    try { selectedDetail = await api.workItemDetail(pid, id); }
    catch (cause) { message = cause instanceof Error ? cause.message : 'Failed to load Work Item'; }
    finally { detailLoading = false; }
  }

  function filtersChanged() {
    queueMicrotask(() => {
      const first = filteredItems[0];
      if (first && !filteredItems.some((item) => item.id === selectedId)) selectItem(first.id, false);
      if (!first) { selectedId = ''; selectedDetail = null; }
    });
  }

  function typeFilterChanged() {
    if (filters.type && !['task', 'bug', 'chore'].includes(filters.type)) filters.readiness = '';
    filtersChanged();
  }

  function openItem(id: string) { if (pid) goto(resolve('/work-item/[id]', { id }) + `?projectId=${pid}`); }
  function total(): number { return summary ? Object.values(summary.typeCounts).reduce((sum, count) => sum + count, 0) : 0; }
  function date(value: string): string { return value ? new Date(value.replace(' ', 'T')).toLocaleDateString() : '—'; }
  function field(row: Record<string, unknown>, key: string): string { return row[key] == null ? '' : String(row[key]); }
</script>

<svelte:head><title>Work Items - pic task system</title></svelte:head>

{#if !pid}
  <p class="empty-state">Select a project.</p>
{:else if loading}
  <div class="loading-list" aria-label="Loading Work Items"><span></span><span></span><span></span></div>
{:else if error}
  <p class="error">{error}</p>
{:else}
  <div class="stats-grid">
    <div class="stat-card"><div class="stat-value">{total()}</div><div class="stat-label">Work Items</div></div>
    <div class="stat-card"><div class="stat-value">{summary?.statusCounts.open ?? 0}</div><div class="stat-label">Open</div></div>
    <div class="stat-card"><div class="stat-value">{summary?.statusCounts.in_progress ?? 0}</div><div class="stat-label">In Progress</div></div>
    <div class="stat-card"><div class="stat-value">{summary?.readinessCounts.ready ?? 0}</div><div class="stat-label">Ready</div></div>
    <div class="stat-card"><div class="stat-value">{summary?.readinessCounts.blocked ?? 0}</div><div class="stat-label">Blocked</div></div>
    <div class="stat-card"><div class="stat-value">{summary?.typeCounts.epic ?? 0}</div><div class="stat-label">Epics</div></div>
  </div>

  <div class="action-bar"><button class="btn btn-primary" onclick={() => showCreate = !showCreate}>New Work Item</button></div>
  {#if showCreate}
    <form class="work-item-form" onsubmit={(event) => { event.preventDefault(); createItem(); }}>
      <label>Type<select bind:value={draft.type}>{#each ['epic','feature','task','bug','chore','gate'] as type (type)}<option value={type}>{type}</option>{/each}</select></label>
      <label class="form-wide">Title<input bind:value={draft.title} maxlength="300" /></label>
      <label>Parent<select bind:value={draft.parent_id}><option value="">None</option>{#each aggregateParents as parent (parent.id)}<option value={parent.id}>{parent.title}</option>{/each}</select></label>
      <label>Priority<select bind:value={draft.priority}><option value="high">High</option><option value="medium">Medium</option><option value="low">Low</option></select></label>
      <label class="form-wide">Description<textarea bind:value={draft.description} rows="2"></textarea></label>
      <label class="form-wide">Labels<input bind:value={draft.labels} placeholder="backend,release-v1" /></label>
      <div class="form-actions"><button class="btn btn-primary" type="submit">Create</button><button class="btn" type="button" onclick={() => showCreate = false}>Cancel</button>{#if message}<span class="error">{message}</span>{/if}</div>
    </form>
  {/if}

  <div id="tabs">
    <button class="tab" class:active={tab === 'work-items'} onclick={() => switchTab('work-items')}>Work Items</button>
    <button class="tab" class:active={tab === 'workflow'} onclick={() => switchTab('workflow')}>Workflow</button>
    <button class="tab" class:active={tab === 'analytics'} onclick={() => switchTab('analytics')}>Analytics</button>
    <button class="tab" class:active={tab === 'routing'} onclick={() => switchTab('routing')}>Routing</button>
  </div>

  {#if tab === 'work-items'}
    <div class="filter-bar">
      <select aria-label="Type" bind:value={filters.type} onchange={typeFilterChanged}><option value="">All types</option>{#each ['epic','feature','task','bug','chore','gate'] as type (type)}<option value={type}>{type}</option>{/each}</select>
      <select aria-label="Status" bind:value={filters.status} onchange={filtersChanged}><option value="">All statuses</option><option value="open">Open</option><option value="in_progress">In progress</option><option value="done">Done</option><option value="cancelled">Cancelled</option></select>
      <select aria-label="Readiness" bind:value={filters.readiness} onchange={filtersChanged} disabled={filters.type !== '' && !['task', 'bug', 'chore'].includes(filters.type)}><option value="">Any readiness</option><option value="ready">Ready</option><option value="blocked">Blocked</option></select>
      <select aria-label="Label" bind:value={filters.label} onchange={filtersChanged}><option value="">All labels</option>{#each labels as entry (entry.label)}<option value={entry.label}>{entry.label} ({entry.count})</option>{/each}</select>
      <input aria-label="Filter Work Items" placeholder="Filter Work Items" bind:value={filters.q} oninput={filtersChanged} />
    </div>
    <div class="work-browser">
      <section class="work-list" aria-label="Work Items">
        <div class="work-list-heading"><strong>{filteredItems.length} Work Items</strong><span>{filters.readiness === 'ready' ? 'Available to start' : 'Matching filters'}</span></div>
        {#if filteredItems.length === 0}<p class="empty-state">No Work Items match these filters.</p>{:else}
          {#each filteredItems as item (item.id)}
            <button class="work-row" class:selected={item.id === selectedId} onclick={() => selectItem(item.id)}>
              <span class="work-row-id">{item.id}</span>
              <strong>{item.title}</strong>
              <span class="work-row-badges"><span class="type-badge {item.type}">{item.type}</span><span class="status-badge {item.status}">{item.status.replace('_', ' ')}</span><span class="readiness" class:ready={readyIds.has(item.id)}>{readyIds.has(item.id) ? 'Ready' : 'Blocked'}</span><span class="priority-{item.priority}">{item.priority}</span></span>
              {#if item.labels.length}<span class="label-list">{#each item.labels as label (label)}<span class="label-chip">{label}</span>{/each}</span>{/if}
            </button>
          {/each}
        {/if}
      </section>

      <aside class="work-preview" aria-label="Selected Work Item">
        {#if detailLoading}<div class="loading-list" aria-label="Loading Work Item"><span></span><span></span><span></span></div>
        {:else if selectedDetail}
          <header><span class="type-badge {selectedDetail.workItem.type}">{selectedDetail.workItem.type}</span><h2>{selectedDetail.workItem.title}</h2><p>{selectedDetail.workItem.id} · Created {date(selectedDetail.workItem.created_at)}</p></header>
          <div class="preview-state"><span class="status-badge {selectedDetail.workItem.status}">{selectedDetail.workItem.status.replace('_', ' ')}</span><span class="readiness" class:ready={selectedDetail.ready}>{selectedDetail.ready ? 'Ready to start' : 'Blocked'}</span><span class="priority-{selectedDetail.workItem.priority}">{selectedDetail.workItem.priority} priority</span></div>
          {#if selectedDetail.workItem.description}<section><h3>Description</h3><p class="long-text">{selectedDetail.workItem.description}</p></section>{/if}
          {#if selectedDetail.workItem.labels.length}<section><h3>Labels</h3><div class="label-list">{#each selectedDetail.workItem.labels as label (label)}<span class="label-chip">{label}</span>{/each}</div></section>{/if}
          <section><h3>{selectedDetail.ready ? 'Start conditions' : 'Blocked by'}</h3>
            {#if selectedDetail.ready}<p class="ready-copy">No unfinished dependencies or unresolved gates. This Work Item can be claimed now.</p>
            {:else if selectedDetail.dependencies.length === 0 && selectedDetail.gates.length === 0}<p class="empty-state compact">Status, deferral, or claim state prevents work.</p>
            {:else}<ul class="preview-blockers">{#each selectedDetail.dependencies as row (field(row, 'depends_on_work_item_id'))}<li><button onclick={() => selectItem(field(row, 'depends_on_work_item_id'), false)}>{field(row, 'title') || field(row, 'depends_on_work_item_id')}</button><span class="status-badge {field(row, 'status')}">{field(row, 'status')}</span></li>{/each}{#each selectedDetail.gates as row (field(row, 'gate_work_item_id'))}<li><button onclick={() => selectItem(field(row, 'gate_work_item_id'), false)}>{field(row, 'title') || field(row, 'gate_work_item_id')}</button><span class="status-badge {field(row, 'status')}">{field(row, 'status')}</span></li>{/each}</ul>{/if}
          </section>
          <dl class="preview-meta"><div><dt>Claimed by</dt><dd>{selectedDetail.workItem.claimed_by || 'Unclaimed'}</dd></div><div><dt>Parent</dt><dd>{selectedDetail.workItem.parent_id || 'None'}</dd></div><div><dt>Children</dt><dd>{selectedDetail.children.length}</dd></div><div><dt>Review</dt><dd>{selectedDetail.workItem.review_status}</dd></div></dl>
          <button class="btn btn-primary preview-open" onclick={() => openItem(selectedDetail.workItem.id)}>Open Work Item</button>
        {:else}<p class="empty-state">Select a Work Item.</p>{/if}
      </aside>
    </div>
  {:else if tab === 'workflow'}
    <div class="workflow-queues">{#each Object.entries(queues) as [name, rows] (name)}<section class="workflow-queue"><h3>{name} <span class="queue-count">{rows.length}</span></h3>{#if rows.length === 0}<p class="queue-empty">None</p>{:else}<ul class="queue-list">{#each rows as row (row.taskId)}<li><button onclick={() => openItem(row.taskId)}><strong>{row.taskTitle}</strong><span>{row.status}</span></button></li>{/each}</ul>{/if}</section>{/each}</div>
  {:else if tab === 'routing'}
    {#if !routing || routing.totalEvents === 0}
      <p class="empty-state">No skill routing events recorded yet. Worker launches record one routing decision each once the scheduler runs.</p>
    {:else}
      <div class="stats-grid">
        <div class="stat-card"><div class="stat-value">{routing.totalEvents}</div><div class="stat-label">Routing Events</div></div>
        <div class="stat-card"><div class="stat-value">{routing.familyCounts.length}</div><div class="stat-label">Families Matched</div></div>
        <div class="stat-card"><div class="stat-value">{routing.missingCounts.reduce((sum, row) => sum + row.count, 0)}</div><div class="stat-label">Missing Occurrences</div></div>
      </div>
      {#if routing.missingCounts.length}
        <section class="routing-section"><h3>Missing families (matched but not selected — enforcement-decision signal)</h3>
          <div class="table-scroll"><table><thead><tr><th>Family</th><th>Occurrences</th></tr></thead><tbody>
            {#each routing.missingCounts as row (row.missing)}<tr><td>{row.missing}</td><td>{row.count}</td></tr>{/each}
          </tbody></table></div>
        </section>
      {/if}
      <section class="routing-section"><h3>Matched families</h3>
        {#if routing.familyCounts.length === 0}<p class="empty-state compact">No family matches recorded.</p>
        {:else}<div class="table-scroll"><table><thead><tr><th>Family</th><th>Matched by</th><th>Count</th></tr></thead><tbody>
          {#each routing.familyCounts as row (`${row.family}:${row.matchedBy}`)}<tr><td>{row.family}</td><td>{tokenList(row.matchedBy)}</td><td>{row.count}</td></tr>{/each}
        </tbody></table></div>{/if}
      </section>
      <section class="routing-section"><h3>Recent routing decisions</h3>
        <div class="table-scroll"><table><thead><tr><th>Work Item</th><th>Stage</th><th>Selected</th><th>Matched</th><th>Missing</th><th>Evidence</th><th>At</th></tr></thead><tbody>
          {#each routing.recentEvents as row (`${row.workItemId}:${row.createdAt}:${row.packId}`)}
            <tr onclick={() => openItem(row.workItemId)} onkeydown={(event) => event.key === 'Enter' && openItem(row.workItemId)} role="button" tabindex="0">
              <td>{row.workItemId}</td><td>{row.stage}</td>
              <td>{jsonList(row.selectedFamilies).join(', ') || '—'}</td>
              <td>{familyMatches(row.matchedFamilies)}</td>
              <td>{jsonList(row.missingFamilies).join(', ') || '—'}</td>
              <td>{jsonList(row.evidenceSources).join(', ') || '—'}</td>
              <td>{date(row.createdAt)}</td>
            </tr>
          {/each}
        </tbody></table></div>
      </section>
    {/if}
  {:else}
    {#if analytics.length === 0}<p class="empty-state">No workflow analytics recorded.</p>{:else}<div class="table-scroll"><table id="analytics-table"><thead><tr><th>Work Item</th><th>Stage</th><th>Status</th><th>Attempts</th><th>Outcome</th></tr></thead><tbody>{#each analytics as row (`${row.taskId}:${row.stage}`)}<tr onclick={() => openItem(row.taskId)} onkeydown={(event) => event.key === 'Enter' && openItem(row.taskId)} role="button" tabindex="0"><td>{row.taskTitle}</td><td>{row.stageLabel}</td><td>{row.status}</td><td>{row.attempts || '—'}</td><td>{row.outcome || '—'}</td></tr>{/each}</tbody></table></div>{/if}
  {/if}
{/if}