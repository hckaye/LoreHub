export type Repository = {
  id: string;
  organizationId: string;
  owner: string;
  slug: string;
  displayName: string;
  description: string;
  visibility: "private" | "internal" | "public";
  loreRepositoryId: string;
  loreUrl: string;
  defaultBranch: string;
  issueCount: number;
  mergeRequestCount: number;
  updatedAt: string;
};

export type Organization = {
  id: string;
  slug: string;
  displayName: string;
  description: string;
  visibility: "private" | "internal" | "public";
  createdAt: string;
};

export type AuthUser = {
  id: string;
  username: string;
  displayName: string;
  email: string | null;
  avatarUrl: string | null;
  locale: string | null;
};

export type AuthSession =
  | {
      status: "authenticated";
      user: AuthUser;
      csrfToken: string;
    }
  | {
      status: "anonymous" | "expired";
      user: null;
    }
  | {
      status: "unavailable";
      user: null;
      reason: "network" | "provider";
    };

export type Branch = {
  id: string;
  name: string;
  category: string;
  latestRevision: string;
  creator: string;
  createdAt: string;
  current: boolean;
  archived: boolean;
};

export type Issue = {
  id: string;
  number: number;
  title: string;
  body: string;
  state: "open" | "closed";
  author: string;
  assignee: string | null;
  commentCount: number;
  createdAt: string;
  updatedAt: string;
};

export type MergeRequest = {
  id: string;
  number: number;
  title: string;
  body: string;
  state: "open" | "closed" | "merged";
  sourceBranch: string;
  targetBranch: string;
  sourceRevision: string;
  targetRevision: string;
  author: string;
  approvalCount: number;
  createdAt: string;
  updatedAt: string;
};

export type CIRun = {
  id: string;
  workflowId: string;
  workflowName: string;
  workflowPath: string;
  runNumber: number;
  runAttempt: number;
  rerunOf?: string;
  eventName: string;
  branch: string;
  revision: string;
  status: "queued" | "in_progress" | "completed" | "cancelled";
  conclusion: "success" | "failure" | "cancelled" | "skipped" | "timed_out" | null;
  queuedAt: string;
  startedAt: string | null;
  completedAt: string | null;
};

export type CIWorkflowDispatchInput = {
  description?: string;
  required?: boolean;
  default?: string;
  type: "string" | "boolean" | "choice" | "number" | "environment";
  options?: string[];
};

export type CIWorkflow = {
  id: string;
  path: string;
  name: string;
  enabled: boolean;
  state: "active" | "disabled" | "error";
  errorCode?: string;
  errorMessage?: string;
  lastSeenRevision: string;
  triggerConfig: {
    push?: { branches?: string[]; branches_ignore?: string[] };
    pull_request?: { branches?: string[]; branches_ignore?: string[]; types?: string[] };
    schedule?: { cron: string }[];
    repository_dispatch?: { types?: string[] };
    workflow_dispatch?: { inputs?: Record<string, CIWorkflowDispatchInput> };
  };
  updatedAt: string;
};

export type CIWorkflowPage = {
  workflows: CIWorkflow[];
  totalCount: number;
  page: number;
  perPage: number;
  hasMore: boolean;
  canWrite: boolean;
};

export type CIRunPage = {
  runs: CIRun[];
  totalCount: number;
  page: number;
  perPage: number;
  hasMore: boolean;
};

export type CIJob = {
  id: string;
  name: string;
  status: "queued" | "in_progress" | "completed" | "cancelled";
  conclusion: CIRun["conclusion"];
  attempt: number;
  logAvailable: boolean;
  queuedAt: string;
  startedAt: string | null;
  completedAt: string | null;
};

export type CIArtifact = {
  id: string;
  jobId: string;
  name: string;
  sizeBytes: number;
  createdAt: string;
};

export type CIRunDetail = {
  run: CIRun;
  workflow: CIWorkflow;
  jobs: CIJob[];
  artifacts: CIArtifact[];
};

export type APIResult<T> =
  | { ok: true; data: T }
  | {
      ok: false;
      reason: "not-found" | "unauthorized" | "forbidden" | "invalid" | "unavailable";
      code?: string;
    };
