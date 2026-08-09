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
  mergedBy?: string | null;
  mergedRevision?: string | null;
  mergedAt?: string | null;
  createdAt: string;
  updatedAt: string;
};

export type TreeEntry = {
  name: string;
  path: string;
  kind: "directory" | "file" | "link";
  mode: number;
  size: number;
};

export type LoreTree = {
  revision: string;
  path: string;
  entries: TreeEntry[];
  hasMore: boolean;
};

export type LoreFile = {
  path: string;
  revision: string;
  kind: "directory" | "file" | "link";
  mode: number;
  size: number;
  binary: boolean;
  binaryKnown: boolean;
  truncated: boolean;
  content?: string;
};

export type RevisionHistoryEntry = {
  revision: string;
  number: number;
  parents: string[];
};

export type FileHistoryEntry = {
  path: string;
  revision: string;
  number: number;
  parents: string[];
  action: string;
  size: number;
};

export type LoreRevision = {
  revision: string;
  number: number;
  parents: string[];
  author?: string;
  createdAt?: string;
  message?: string;
};

export type LoreDiffFile = {
  path: string;
  action: string;
  patch?: string;
  binary: boolean;
  binaryKnown: boolean;
  truncated: boolean;
};

export type LoreDiff = {
  source: string;
  target: string;
  files: LoreDiffFile[];
  hasMore: boolean;
  truncated: boolean;
};

export type Review = {
  id: string;
  mergeRequestId: string;
  reviewer: string;
  sourceRevision: string;
  decision: "approved" | "changes_requested" | "commented";
  body: string;
  createdAt: string;
};

export type ReviewSummary = {
  currentRevision: string;
  reviews: Review[];
  currentReviews: Review[];
  approvals: number;
  changeRequests: number;
  comments: number;
};

export type MergeBlocker = { code: string; detail: string };

export type MergeOperation = {
  id: string;
  mergeRequestId: string;
  repositoryId: string;
  sourceRevision: string;
  targetRevision: string;
  stagedRevision?: string;
  pushedRevision?: string;
  state: "created" | "started" | "conflicts" | "ready_to_push" | "pushing" | "pushed" | "aborted" | "merged";
  conflictPaths: string[];
  errorCode?: string;
  errorDetail?: string;
  version: number;
  leaseExpiresAt?: string;
  startedAt?: string;
  completedAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type MergeReadiness = {
  mergeRequest: MergeRequest;
  currentSourceRevision: string;
  currentTargetRevision: string;
  sourceStale: boolean;
  targetStale: boolean;
  canMerge: boolean;
  ready: boolean;
  blockers: MergeBlocker[];
  reviews: ReviewSummary;
  ciSuccess: boolean;
  directPushBlocked: boolean;
  rules: Array<{
    id: string;
    pattern: string;
    requiredApprovals: number;
    requireCiSuccess: boolean;
    blockDirectPush: boolean;
  }>;
  operation?: MergeOperation;
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
