<script lang="ts">
  import '../app.css';
  import { onMount, onDestroy } from 'svelte';
  import { projects, currentProjectId, connectionStatus, refreshIntervalMs } from '$lib/stores.js';
  import { api } from '$lib/api.js';
  import { goto } from '$app/navigation';

  let projList: import('$lib/api.js').Project[] = $state([]);
  let selProjId: string | null = $state(null);
  let connStatus: string = $state('loading');
  let refreshMs: number = $state(0);
  let refreshTimer: ReturnType<typeof setInterval> | null = null;

  let searchQuery = $state('');
  let searchTimeout: ReturnType<typeof setTimeout> | null = null;

  projects.subscribe((v) => projList = v);
  currentProjectId.subscribe((v) => selProjId = v);
  connectionStatus.subscribe((v) => connStatus = v);
  refreshIntervalMs.subscribe((v) => {
    refreshMs = v;
    if (refreshTimer) clearInterval(refreshTimer);
    refreshTimer = null;
    if (v > 0) {
      refreshTimer = setInterval(() => {
        loadProjects();
        if (selProjId) refreshDashboard(selProjId);
      }, v);
    }
  });

  async function loadProjects() {
    connectionStatus.set('loading');
    try {
      const data = await api.projects();
      projects.set(data.projects);
      connectionStatus.set('ok');
    } catch {
      connectionStatus.set('error');
    }
  }

  async function refreshDashboard(pid: string) {
    try { await api.projectSummary(pid); } catch {}
  }

  async function selectProject(id: string) {
    currentProjectId.set(id);
    await goto('/dashboard');
    loadProjects();
  }

  function onSearchInput() {
    if (searchTimeout) clearTimeout(searchTimeout);
    if (!searchQuery.trim()) return;
    searchTimeout = setTimeout(() => {
      goto(`/search?q=${encodeURIComponent(searchQuery.trim())}`);
    }, 250);
  }

  function onSearchKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && searchTimeout) {
      clearTimeout(searchTimeout);
      searchTimeout = null;
      goto(`/search?q=${encodeURIComponent(searchQuery.trim())}`);
    }
  }

  onMount(() => {
    loadProjects();
  });

  onDestroy(() => {
    if (refreshTimer) clearInterval(refreshTimer);
  });
</script>

<div id="header">
  <div class="header-left">
    <h1 id="app-title">pic task system</h1>
    <div id="search-container">
      <input
        type="search"
        id="search-input"
        placeholder="Search across projects..."
        aria-label="Global search"
        bind:value={searchQuery}
        oninput={onSearchInput}
        onkeydown={onSearchKeydown}
      />
    </div>
  </div>
  <div class="header-right">
    <select
      id="refresh-select"
      aria-label="Auto-refresh interval"
      value={refreshMs}
      onchange={(e) => refreshIntervalMs.set(Number((e.target as HTMLSelectElement).value))}
    >
      <option value="0">off</option>
      <option value="5000">5s</option>
      <option value="15000">15s</option>
      <option value="60000">60s</option>
    </select>
    <span id="connection-status" class="status-{connStatus}">●</span>
  </div>
</div>

<div id="layout">
  <aside id="sidebar">
    <h2>Projects</h2>
    <ul id="project-list">
      {#each projList as proj}
        <li
          class:active={proj.id === selProjId}
          onclick={() => selectProject(proj.id)}
          role="button"
          tabindex="0"
          onkeydown={(e) => e.key === 'Enter' && selectProject(proj.id)}
        >
          <span class="project-indicator {proj.health}"></span>
          <span class="project-name">{proj.name}</span>
        </li>
      {/each}
    </ul>
  </aside>

  <main id="main-content">
    <slot />
  </main>
</div>
