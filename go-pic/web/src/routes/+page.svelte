<svelte:head>
  <title>pic task-system dashboard</title>
</svelte:head>

<div id="welcome-view" class="view">
  <h2>Welcome</h2>
  <p>Select a project from the sidebar to view its dashboard.</p>
</div>

<style>
  #welcome-view {
    text-align: center;
    padding-top: 60px;
  }
  #welcome-view h2 {
    font-size: 20px;
    margin-bottom: 8px;
    color: var(--accent);
  }
  #welcome-view p {
    color: var(--text-muted);
    font-size: 14px;
  }
</style>
