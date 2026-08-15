const BASE = '';

async function request<T>(path: string, opts?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json', ...opts?.headers },
    ...opts,
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || res.statusText);
  }
  return res.json();
}

export interface Project {
  id: string;
  name: string;
  rootPath: string;
  databasePath: string;
  health: string;
  createdAt: string;
  updatedAt: string;
}

export interface WorkItem {
  id: string;
  type: 'epic' | 'feature' | 'task' | 'bug' | 'chore' | 'gate';
  parent_id: string | null;
  title: string;
  description: string;
  status: 'open' | 'in_progress' | 'done' | 'cancelled';
  priority: 'low' | 'medium' | 'high';
  deferred: number;
  claimed_at: string;
  claimed_by: string;
  review_status: string;
  review_notes: string;
  created_at: string;
  labels: string[];
  depth?: number;
}

export interface WorkItemLabel { label: string; count: number; }

export interface ProjectSummary {
  projectId: string;
  projectName: string;
  rootPath: string;
  health: string;
  statusCounts: Record<string, number>;
  typeCounts: Record<string, number>;
  priorityCounts: Record<string, number>;
  reviewCounts: Record<string, number>;
  readinessCounts: Record<string, number>;
  latestActivity: string;
}

export interface WorkItemDetail {
  workItem: WorkItem;
  ready: boolean;
  children: WorkItem[];
  descendants: WorkItem[];
  dependencies: Array<Record<string, unknown>>;
  gates: Array<Record<string, unknown>>;
  artifacts: Array<Record<string, unknown>>;
  checkpoints: Array<Record<string, unknown>>;
  instructionPacks: Array<Record<string, unknown>>;
  authorizations: Array<Record<string, unknown>>;
  completionReports: Array<Record<string, unknown>>;
  verificationReports: Array<Record<string, unknown>>;
}

export interface WorkflowAnalyticsRow {
  taskId: string;
  taskTitle: string;
  epicTitle: string;
  workflowMode: string;
  stage: string;
  stageLabel: string;
  stageOrder: number;
  status: string;
  startedAt: string;
  completedAt: string;
  elapsedSeconds: number | null;
  attempts: number;
  outcome: string;
}

export interface SearchResult {
  type: WorkItem['type'];
  id: string;
  title: string;
  content: string;
  parentId?: string;
  projectId: string;
  projectName: string;
}

export interface WorkflowItem {
  taskId: string;
  taskTitle: string;
  epicTitle: string;
  projectId: string;
  projectName: string;
  type: string;
  status: string;
  createdAt: string;
}

export interface ActivityRow {
  session_id: string;
  task_id: string;
  task_title: string;
  status: string;
  done: number;
  total: number;
  last_skill: string;
  updated_at: string;
}

export const api = {
  projects: () => request<{ projects: Project[] }>('/api/projects'),
  projectSummary: (projectId: string) => request<ProjectSummary>(`/api/projects/${projectId}/summary`),
  workflowAnalytics: (projectId: string) => request<{ rows: WorkflowAnalyticsRow[] }>(`/api/projects/${projectId}/workflow-analytics`),
  workItems: (projectId: string, label = '') => request<{ workItems: WorkItem[] }>(`/api/projects/${projectId}/work-items${label ? `?label=${encodeURIComponent(label)}` : ''}`),
  workItemLabels: (projectId: string) => request<{ labels: WorkItemLabel[] }>(`/api/projects/${projectId}/work-items/labels`),
  readyWorkItems: (projectId: string) => request<{ workItems: WorkItem[] }>(`/api/projects/${projectId}/work-items/ready`),
  workItemDetail: (projectId: string, id: string) => request<WorkItemDetail>(`/api/projects/${projectId}/work-items/${id}`),
  createWorkItem: (projectId: string, input: Pick<WorkItem, 'type' | 'title' | 'description' | 'priority'> & { parent_id?: string; labels?: string[] }) =>
    request<{ workItem: WorkItem }>(`/api/projects/${projectId}/work-items`, { method: 'POST', body: JSON.stringify(input) }),
  addWorkItemLabels: (projectId: string, id: string, labels: string[]) =>
    request<{ workItem: WorkItem }>(`/api/projects/${projectId}/work-items/${id}/labels`, { method: 'POST', body: JSON.stringify({ labels }) }),
  removeWorkItemLabels: (projectId: string, id: string, labels: string[]) =>
    request<{ workItem: WorkItem }>(`/api/projects/${projectId}/work-items/${id}/labels`, { method: 'DELETE', body: JSON.stringify({ labels }) }),
  updateWorkItemStatus: (projectId: string, id: string, status: WorkItem['status']) =>
    request<{ workItem: WorkItem }>(`/api/projects/${projectId}/work-items/${id}/status`, { method: 'PATCH', body: JSON.stringify({ status }) }),
  search: (query: string) => request<{ query: string; results: SearchResult[]; totalCount: number }>(`/api/search?q=${encodeURIComponent(query)}`),
  workflowQueue: (queueName: string) => request<{ items: WorkflowItem[] }>(`/api/workflow/${queueName}`),
  activity: (projectId: string) => request<{ activity: ActivityRow[] }>(`/api/projects/${projectId}/activity`),
};