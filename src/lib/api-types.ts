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
  runNumber: number;
  eventName: string;
  branch: string;
  revision: string;
  status: "queued" | "in_progress" | "completed" | "cancelled";
  conclusion: "success" | "failure" | "cancelled" | "skipped" | "timed_out" | null;
  queuedAt: string;
  startedAt: string | null;
  completedAt: string | null;
};

export type APIResult<T> =
  | { ok: true; data: T }
  | {
      ok: false;
      reason: "not-found" | "unauthorized" | "forbidden" | "invalid" | "unavailable";
      code?: string;
    };
