import type { MergeStatusCheck } from "./commit-status-types";

export type { MergeStatusCheck, RevisionStatus, RevisionStatusPage, RevisionStatusState } from "./commit-status-types";

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
  homepageUrl: string;
  allowIssues: boolean;
  allowMergeRequests: boolean;
  topics: string[];
  issueCount: number;
  mergeRequestCount: number;
  starCount?: number;
  watcherCount?: number;
  viewerHasStarred?: boolean;
  viewerIsWatching?: boolean;
  archivedAt: string | null;
  updatedAt: string;
};

export type DeletedRepository = {
  id: string;
  organizationId: string;
  owner: string;
  slug: string;
  displayName: string;
  requestedBy: string;
  requestedAt: string;
  purgeAfter: string;
  purging: boolean;
};

export type RepositoryEngagement = {
  starCount: number;
  watcherCount: number;
  viewerHasStarred: boolean;
  viewerIsWatching: boolean;
};

export type Organization = {
  id: string;
  slug: string;
  displayName: string;
  description: string;
  visibility: "private" | "internal" | "public";
  createdAt: string;
};

export type OrganizationView = Organization & {
  websiteUrl: string;
  contactEmail: string;
  defaultRepositoryVisibility: "private" | "internal" | "public";
  role: "" | "owner" | "maintainer" | "member";
  memberCount: number;
  repositoryCount: number;
  teamCount: number;
};

export type AuditActor = {
  id: string;
  username: string;
  displayName: string;
};

export type AuditRepository = {
  id: string;
  owner: string;
  slug: string;
};

export type AuditEvent = {
  id: string;
  action: string;
  targetType: string;
  targetId: string | null;
  actor: AuditActor | null;
  repository: AuditRepository | null;
  remoteAddress: string | null;
  details: Record<string, unknown>;
  occurredAt: string;
};

export type AuditLogPage = {
  items: AuditEvent[];
  nextCursor: string | null;
};

export type RepositoryInsightCurrent = {
  openIssues: number;
  openPullRequests: number;
  branches: number;
  publishedReleases: number;
};

export type RepositoryInsightPeriod = {
  issuesOpened: number;
  issuesClosed: number;
  pullRequestsOpened: number;
  pullRequestsMerged: number;
  workflowRunsCompleted: number;
  workflowRunsSucceeded: number;
  releasesPublished: number;
  branchPushes: number;
};

export type RepositoryInsightDay = Omit<RepositoryInsightPeriod, "workflowRunsSucceeded"> & {
  date: string;
};

export type RepositoryInsightContributor = {
  id: string;
  username: string;
  displayName: string;
  activityCount: number;
  lastActiveAt: string;
};

export type RepositoryInsights = {
  periodDays: 7 | 30 | 90;
  periodStart: string;
  periodEnd: string;
  current: RepositoryInsightCurrent;
  period: RepositoryInsightPeriod;
  activity: RepositoryInsightDay[];
  contributors: RepositoryInsightContributor[];
};

export type UserProfile = {
  id: string;
  username: string;
  displayName: string;
  email: string | null;
  bio: string;
  avatarUrl: string;
  websiteUrl: string;
  location: string;
  company: string;
  pronouns: string;
  locale: string;
  createdAt: string;
  repositoryCount: number;
};

export type Team = {
  id: string;
  organizationId: string;
  organizationSlug: string;
  slug: string;
  displayName: string;
  description: string;
  viewerRole: "" | "owner" | "maintainer" | "member";
  memberCount: number;
  createdAt: string;
  updatedAt: string;
};

export type TeamMember = {
  userId: string;
  username: string;
  displayName: string;
  role: "maintainer" | "member";
  joinedAt: string;
};

export type Notification = {
  id: string;
  topic: string;
  title: string;
  body: string;
  href: string;
  readAt: string | null;
  createdAt: string;
};

export type NotificationPage = {
  items: Notification[];
  nextCursor: string | null;
};

export type NotificationPreferences = {
  inAppEnabled: boolean;
  emailEnabled: boolean;
  emailAvailable: boolean;
  mentionEnabled: boolean;
  teamEnabled: boolean;
  repositoryEnabled: boolean;
  updatedAt: string;
};

export type DashboardData = {
  repositories: Repository[];
  organizations: OrganizationView[];
  notifications: Notification[];
  unreadNotifications: number;
};

export type AuthProvider = {
  id: "password" | "google" | "github" | "facebook" | "x";
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

export type BranchOverview = {
  branches: Branch[];
  viewerCanPush: boolean;
  viewerCanManageRules: boolean;
};

export type BranchRule = {
  id: string;
  repositoryId: string;
  pattern: string;
  requiredApprovals: number;
  requireCiSuccess: boolean;
  requiredStatusChecks: string[];
  blockDirectPush: boolean;
  createdAt: string;
  updatedAt: string;
};

export type MilestoneSummary = {
  id: string;
  number: number;
  title: string;
  state: "open" | "closed";
  dueOn: string | null;
};

export type Assignee = {
  id: string;
  username: string;
  displayName: string;
  avatarUrl: string;
};

export type AssigneePage = {
  items: Assignee[];
  nextCursor?: string;
  hasMore: boolean;
};

export type Issue = {
  id: string;
  number: number;
  title: string;
  body: string;
  state: "open" | "closed";
  author: string;
  assignee: string | null;
  assignees: Assignee[];
  labels: Label[];
  milestone: MilestoneSummary | null;
  labelCount: number;
  commentCount: number;
  createdAt: string;
  updatedAt: string;
  closedBy: string | null;
  closedAt: string | null;
  viewerCanUpdate: boolean;
  viewerCanManageLabels: boolean;
  viewerCanManageMilestone: boolean;
  viewerCanManageAssignees: boolean;
};

export type RepositoryIssuePage = {
  issues: Issue[];
  totalCount: number;
  openCount: number;
  closedCount: number;
  page: number;
  perPage: number;
  hasNext: boolean;
};

export type IssueComment = {
  id: string;
  issueId: string;
  author: string;
  body: string;
  createdAt: string;
  editedAt: string | null;
  viewerCanUpdate: boolean;
};

export type DiscussionCategory = {
  id: string;
  slug: string;
  name: string;
  description: string;
  format: "discussion" | "question" | "announcement";
  discussionCount: number;
  createdAt: string;
  updatedAt: string;
};

export type DiscussionAuthor = {
  id: string;
  username: string;
  displayName: string;
  avatarUrl: string;
};

export type DiscussionSummary = {
  id: string;
  number: number;
  title: string;
  state: "open" | "closed";
  locked: boolean;
  pinned: boolean;
  answered: boolean;
  category: DiscussionCategory;
  author: DiscussionAuthor;
  commentCount: number;
  voteCount: number;
  viewerHasVoted: boolean;
  viewerCanVote: boolean;
  viewerCanEdit: boolean;
  viewerCanModerate: boolean;
  createdAt: string;
  updatedAt: string;
};

export type DiscussionComment = {
  id: string;
  parentId: string | null;
  author: DiscussionAuthor;
  body: string;
  answer: boolean;
  viewerCanEdit: boolean;
  viewerCanDelete: boolean;
  viewerCanMarkAnswer: boolean;
  createdAt: string;
  updatedAt: string;
  editedAt: string | null;
};

export type Discussion = DiscussionSummary & {
  body: string;
  comments: DiscussionComment[];
  commentPage: number;
  commentsPerPage: number;
  totalComments: number;
  viewerCanComment: boolean;
};

export type DiscussionPage = {
  discussions: DiscussionSummary[];
  totalCount: number;
  page: number;
  perPage: number;
  viewerCanCreate: boolean;
};

export type DiscussionCategoriesPage = {
  categories: DiscussionCategory[];
  viewerCanManage: boolean;
  viewerCanModerate: boolean;
};

export type Label = {
  id: string;
  repositoryId: string;
  name: string;
  description: string;
  color: string;
  createdAt: string;
};

export type LabelPage = {
  items: Label[];
  nextCursor?: string;
  hasMore: boolean;
  viewerCanWrite: boolean;
};

export type MergeRequest = {
  id: string;
  number: number;
  title: string;
  body: string;
  state: "open" | "closed" | "merged";
  isDraft: boolean;
  sourceBranch: string;
  targetBranch: string;
  sourceRevision: string;
  targetRevision: string;
  author: string;
  approvalCount: number;
  mergedBy: string | null;
  mergedRevision: string | null;
  mergedAt: string | null;
  createdAt: string;
  updatedAt: string;
  closedAt: string | null;
  viewerCanUpdate: boolean;
  viewerCanReview: boolean;
};

export type MergeRequestListItem = MergeRequest & {
  labels: Label[];
  assignees: Assignee[];
  milestone: MilestoneSummary | null;
  commentCount: number;
};

export type RepositoryMergeRequestPage = {
  mergeRequests: MergeRequestListItem[];
  totalCount: number;
  openCount: number;
  closedCount: number;
  mergedCount: number;
  page: number;
  perPage: number;
  hasNext: boolean;
};

export type MergeRequestComment = {
  id: string;
  mergeRequestId: string;
  author: string;
  body: string;
  createdAt: string;
  editedAt: string | null;
  viewerCanUpdate: boolean;
};

export type ReviewThreadComment = {
  id: string;
  author: string;
  body: string;
  deleted: boolean;
  version: number;
  createdAt: string;
  updatedAt: string;
  editedAt?: string | null;
  viewerCanUpdate: boolean;
};

export type ReviewThread = {
  id: string;
  path: string;
  side: "left" | "right";
  lineNumber: number;
  lineContent: string;
  baseRevision: string;
  headRevision: string;
  outdated: boolean;
  resolved: boolean;
  version: number;
  createdBy: string;
  resolvedBy?: string | null;
  createdAt: string;
  updatedAt: string;
  resolvedAt?: string | null;
  viewerCanResolve: boolean;
  comments: ReviewThreadComment[];
};

export type ProjectSummary = {
  id: string;
  number: number;
  title: string;
  description: string;
  state: "open" | "closed";
  createdBy: string;
  columnCount: number;
  itemCount: number;
  createdAt: string;
  updatedAt: string;
};

export type ProjectItem = {
  id: string;
  columnId: string;
  kind: "issue" | "merge_request" | "draft";
  number: number | null;
  title: string;
  body: string;
  state: "open" | "closed" | "merged" | "draft";
  author: string;
  position: number;
  createdAt: string;
  updatedAt: string;
};

export type ProjectColumn = {
  id: string;
  name: string;
  position: number;
  items: ProjectItem[];
  createdAt: string;
  updatedAt: string;
};

export type Project = ProjectSummary & {
  columns: ProjectColumn[];
  viewerCanWrite: boolean;
};

export type MergeRequestMetadata = {
  labels: Label[];
  assignees: Assignee[];
  milestone: MilestoneSummary | null;
  viewerCanManageLabels: boolean;
  viewerCanManageAssignees: boolean;
  viewerCanManageMilestone: boolean;
};

export type ProjectList = {
  projects: ProjectSummary[];
  viewerCanWrite: boolean;
};

export type WikiPageSummary = {
  id: string;
  slug: string;
  title: string;
  version: number;
  createdBy: string;
  updatedBy: string;
  createdAt: string;
  updatedAt: string;
};

export type WikiPage = WikiPageSummary & {
  body: string;
  viewerCanWrite: boolean;
};

export type WikiPageList = {
  pages: WikiPageSummary[];
  viewerCanWrite: boolean;
};

export type WikiRevision = {
  version: number;
  slug: string;
  title: string;
  body?: string;
  editSummary: string;
  editedBy: string;
  createdAt: string;
};

export type ReleaseAsset = {
  id: string;
  name: string;
  externalUrl: string;
  createdBy: string;
  createdAt: string;
};

export type Release = {
  id: string;
  tagName: string;
  title: string;
  notes: string;
  sourceBranch: string;
  revision: string;
  state: "draft" | "published";
  createdBy: string;
  publishedBy: string | null;
  publishedAt: string | null;
  version: number;
  assets: ReleaseAsset[];
  createdAt: string;
  updatedAt: string;
  viewerCanWrite: boolean;
};

export type ReleasePage = {
  releases: Release[];
  page: number;
  perPage: number;
  hasNext: boolean;
  viewerCanWrite: boolean;
};

export type RepositoryTag = {
  name: string;
  revision: string;
  createdAt: string;
  createdBy: string;
};

export type Milestone = MilestoneSummary & {
  description: string;
  createdBy: string;
  closedBy: string | null;
  closedAt: string | null;
  openIssueCount: number;
  closedIssueCount: number;
  version: number;
  createdAt: string;
  updatedAt: string;
  viewerCanWrite: boolean;
};

export type MilestonePage = {
  milestones: Milestone[];
  page: number;
  perPage: number;
  hasNext: boolean;
  viewerCanWrite: boolean;
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

export type ReviewRequest = {
  id: string;
  kind: "user" | "team";
  slug: string;
  displayName: string;
  avatarUrl?: string;
  status: "pending" | Review["decision"];
  requestedBy: string;
  requestedAt: string;
};

export type ReviewRequestSummary = {
  items: ReviewRequest[];
  viewerCanManage: boolean;
};

export type ReviewCandidate = {
  kind: "user" | "team";
  slug: string;
  displayName: string;
  avatarUrl?: string;
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
  parentRevisions: string[];
  resolutions: MergeResolution[];
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

export type MergeResolution = {
  path: string;
  strategy: "mine" | "theirs";
  actor?: string;
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
  statusChecks: MergeStatusCheck[];
  directPushBlocked: boolean;
  rules: BranchRule[];
  operation?: MergeOperation;
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
    environment?: string;
  };
  updatedAt: string;
};

export type ActionsEnvironmentReviewer = {
  userId: string;
  username: string;
  displayName: string;
};

export type ActionsEnvironment = {
  id: string;
  name: string;
  waitTimerMinutes: number;
  preventSelfReview: boolean;
  reviewers: ActionsEnvironmentReviewer[];
  createdAt: string;
  updatedAt: string;
};

export type Deployment = {
  id: string;
  environmentName: string;
  runNumber: number;
  workflowName: string;
  branch: string;
  revision: string;
  status: "pending" | "waiting" | "queued" | "in_progress" | "success" | "failure" | "cancelled" | "rejected";
  actorUsername?: string;
  reviewedUsername?: string;
  canReview: boolean;
  waitUntil: string;
  reviewedAt: string | null;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
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

export type CodeScanningSeverity = "none" | "note" | "warning" | "error";

export type SARIFUploadMetadata = {
  id: string;
  repositoryId: string;
  runId: string;
  jobId: string;
  attempt: number;
  tools: string[];
  revision: string;
  ref: string;
  version: "2.1.0";
  documentSize: number;
  resultsCount: number;
  createdAt: string;
};

export type CodeScanningAlert = {
  id: string;
  uploadId: string;
  repositoryId: string;
  tool: string;
  ruleId: string;
  level: CodeScanningSeverity;
  message: string;
  path: string;
  startLine?: number;
  createdAt: string;
};

export type PersonalAccessTokenScope = "read_api" | "api" | "read_repository" | "write_repository";

export type PersonalAccessToken = {
  id: string;
  name: string;
  prefix: string;
  scopes: PersonalAccessTokenScope[];
  expiresAt: string;
  lastUsedAt: string | null;
  revokedAt: string | null;
  createdAt: string;
};

export type PersonalAccessTokenPage = {
  tokens: PersonalAccessToken[];
};

export type CreatedPersonalAccessToken = {
  token: PersonalAccessToken;
  value: string;
};

export type APIResult<T> =
  | { ok: true; data: T }
  | {
      ok: false;
      reason: "not-found" | "unauthorized" | "forbidden" | "invalid" | "unavailable";
      code?: string;
    };
