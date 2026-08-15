/**
 * Svelte stores for pic task-system shared state.
 */

import { writable } from 'svelte/store';
import type { Project, WorkItem } from './api.js';

/** All loaded projects */
export const projects = writable<Project[]>([]);

/** Currently selected project ID */
export const currentProjectId = writable<string | null>(null);

/** Connection health status */
export const connectionStatus = writable<'ok' | 'error' | 'loading'>('loading');

/** Work Items for the current project */
export const workItems = writable<WorkItem[]>([]);

/** Auto-refresh interval in ms (0 = off) */
export const refreshIntervalMs = writable(0);

/** Currently active tab */
export const activeTab = writable<string>('work-items');
