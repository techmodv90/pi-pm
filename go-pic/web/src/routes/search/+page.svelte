<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { currentProjectId } from '$lib/stores.js';
  import { api } from '$lib/api.js';
  import type { SearchResult } from '$lib/api.js';

  let query = $state('');
  let results = $state<SearchResult[]>([]);
  let totalCount = $state(0);
  let loading = $state(false);
  let error = $state('');

  page.subscribe((p) => {
    const q = p.url.searchParams.get('q') || '';
    if (q && q !== query) {
      query = q;
      doSearch(q);
    } else if (!q) {
      query = '';
      results = [];
    }
  });

  async function doSearch(q: string) {
    if (!q.trim()) return;
    loading = true;
    error = '';
    try {
      const data = await api.search(q);
      results = data.results;
      totalCount = data.totalCount;
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : 'Search failed';
      results = [];
    } finally {
      loading = false;
    }
  }

  function navigateToResult(r: SearchResult) {
    currentProjectId.set(r.projectId);
    goto(`/work-item/${r.id}?projectId=${r.projectId}`);
  }
</script>

<svelte:head>
  <title>Search - pic task system</title>
</svelte:head>

<h2>Search Results</h2>

<p id="search-count">
  {#if loading}
    Searching...
  {:else if query}
    Found {totalCount} result(s)
  {/if}
</p>

{#if error}
  <p class="error">{error}</p>
{:else if results.length === 0 && !loading}
  {#if query}
    <p class="empty-state">No results found for "{query}".</p>
  {:else}
    <p class="empty-state">Type a query in the search bar to search across projects.</p>
  {/if}
{:else}
  <div id="search-results">
    {#each results as r}
      <div class="search-result" onclick={() => navigateToResult(r)} role="button" tabindex="0"
           onkeydown={(e) => e.key === 'Enter' && navigateToResult(r)}>
        <div class="search-type">{r.type}</div>
        <div class="search-title">{r.title || r.content || ''}</div>
        <div class="search-meta">
          {r.projectName}
        </div>
      </div>
    {/each}
  </div>
{/if}
