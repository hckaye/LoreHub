import "server-only";

import { cookies } from "next/headers";

import type {
  APIResult,
  ActionsEnvironment,
  AssigneePage,
  AuditLogPage,
  Branch,
  BranchOverview,
  BranchRule,
  CIRun,
  CIRunDetail,
  CIRunPage,
  CIWorkflowPage,
  CodeScanningAlert,
  DashboardData,
  DeletedRepository,
  Deployment,
  Discussion,
  DiscussionCategoriesPage,
  DiscussionPage,
  FileHistoryEntry,
  Issue,
  IssueComment,
  Label,
  LabelPage,
  LoreDiff,
  LoreFile,
  LoreRevision,
  LoreTree,
  MergeOperation,
  MergeReadiness,
  MergeRequest,
  MergeRequestComment,
  MergeRequestMetadata,
  MilestonePage,
  Notification,
  NotificationPage,
  NotificationPreferences,
  OrganizationView,
  PersonalAccessTokenPage,
  Project,
  ProjectList,
  ReleasePage,
  Repository,
  RepositoryInsights,
  RepositoryIssuePage,
  RepositoryMergeRequestPage,
  ReviewCandidate,
  ReviewRequestSummary,
  ReviewSummary,
  ReviewThread,
  RevisionHistoryEntry,
  RevisionStatusPage,
  SARIFUploadMetadata,
  Team,
  TeamMember,
  UserProfile,
  WikiPage,
  WikiPageList,
  WikiRevision,
} from "./api-types";
import { parseBranchRuleList } from "./branch-rule-contract";
import { parseIssueCommentPage, parseMergeRequestCommentPage } from "./comment-page-contract";
import type { CommentPage } from "./comment-page-types";
import { conversationCommentPageSize } from "./comment-pagination";
import { parseRevisionStatusResponse } from "./commit-status-client";
import { normalizeFileLockPage, type FileLockPage } from "./file-locks";
import { normalizeGlobalWorkItemPage, type GlobalWorkItemPage, type GlobalWorkItemQuery } from "./global-work-items";
import { parseMergeReadiness } from "./merge-readiness-contract";
import { normalizePersonalAccessTokenPage } from "./personal-access-token";
import { parseRepositoryIssuePage, parseRepositoryMergeRequestPage } from "./repository-work-item-contract";
import {
  repositoryWorkItemSearchParams,
  type RepositoryIssueQuery,
  type RepositoryMergeRequestQuery,
} from "./repository-work-item-query";
import { parseSearchResults, searchPageSize, type SearchResults, type SearchType } from "./search";

export type {
  IssueFilter,
  MergeRequestFilter,
  RepositoryIssueQuery,
  RepositoryMergeRequestQuery,
  RepositoryWorkItemDirection,
  RepositoryWorkItemSort,
} from "./repository-work-item-query";

const apiOrigin = process.env.LOREHUB_API_URL ?? "http://127.0.0.1:8080";

export function getDashboard(): Promise<APIResult<DashboardData>> {
  return request("/api/v1/dashboard");
}

export function getGlobalIssues(query: GlobalWorkItemQuery): Promise<APIResult<GlobalWorkItemPage>> {
  return getGlobalWorkItems("issues", query);
}

export function getGlobalPullRequests(query: GlobalWorkItemQuery): Promise<APIResult<GlobalWorkItemPage>> {
  return getGlobalWorkItems("pulls", query);
}

async function getGlobalWorkItems(
  resource: "issues" | "pulls",
  query: GlobalWorkItemQuery,
): Promise<APIResult<GlobalWorkItemPage>> {
  const search = queryString({
    state: query.state,
    scope: query.scope,
    q: query.q?.trim(),
    cursor: query.cursor,
    limit: "25",
  });
  const result = await request<unknown>(`/api/v1/${resource}?${search}`);
  if (!result.ok) return result;
  const page = normalizeGlobalWorkItemPage(result.data);
  return page ? { ok: true, data: page } : { ok: false, reason: "unavailable" };
}

export async function getFileLocks(
  owner: string,
  repository: string,
  branch?: string,
): Promise<APIResult<FileLockPage>> {
  const query = queryString({ branch });
  const result = await request<unknown>(repositoryPath(owner, repository, `/locks${query ? `?${query}` : ""}`));
  if (!result.ok) return result;
  const page = normalizeFileLockPage(result.data);
  return page ? { ok: true, data: page } : { ok: false, reason: "unavailable" };
}

export async function getSearchResults(
  query: string,
  type: SearchType = "all",
  page = 1,
): Promise<APIResult<SearchResults>> {
  const params = new URLSearchParams({ q: query.trim(), type, page: String(page), per_page: String(searchPageSize) });
  const result = await request<unknown>(`/api/v1/search?${params.toString()}`);
  if (!result.ok) return result;
  const data = parseSearchResults(result.data, { q: query.trim(), type, page });
  return data ? { ok: true, data } : { ok: false, reason: "unavailable" };
}

export function getUserProfile(username: string): Promise<APIResult<UserProfile>> {
  return request(`/api/v1/users/${encodeURIComponent(username)}`);
}

export async function getUserRepositories(username: string): Promise<APIResult<Repository[]>> {
  const result = await request<{ repositories: Repository[] }>(
    `/api/v1/users/${encodeURIComponent(username)}/repositories`,
  );
  return result.ok ? { ok: true, data: result.data.repositories } : result;
}

export async function getNotifications(): Promise<APIResult<Notification[]>> {
  const result = await request<NotificationPage>("/api/v1/notifications?limit=100");
  return result.ok ? { ok: true, data: result.data.items } : result;
}

export function getNotificationPreferences(): Promise<APIResult<NotificationPreferences>> {
  return request("/api/v1/account/notification-preferences");
}

export async function getPersonalAccessTokens(): Promise<APIResult<PersonalAccessTokenPage>> {
  const result = await request<unknown>("/api/v1/account/personal-access-tokens");
  if (!result.ok) return result;
  const page = normalizePersonalAccessTokenPage(result.data);
  return page ? { ok: true, data: page } : { ok: false, reason: "unavailable" };
}

export function getOrganization(slug: string): Promise<APIResult<OrganizationView>> {
  return request(`/api/v1/organizations/${encodeURIComponent(slug)}`);
}

export function getOrganizationAuditLog(slug: string, query: string, before: string): Promise<APIResult<AuditLogPage>> {
  const search = queryString({ query: query.trim(), before, perPage: "50" });
  return request(`/api/v1/organizations/${encodeURIComponent(slug)}/audit-log?${search}`);
}

export function getRepositoryInsights(
  owner: string,
  repository: string,
  days: string,
): Promise<APIResult<RepositoryInsights>> {
  const search = new URLSearchParams({ days });
  return request(
    `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/insights?${search}`,
  );
}

export async function getOrganizationRepositories(slug: string): Promise<APIResult<Repository[]>> {
  const result = await request<{ repositories: Repository[] }>(
    `/api/v1/organizations/${encodeURIComponent(slug)}/repositories`,
  );
  return result.ok ? { ok: true, data: result.data.repositories } : result;
}

export function getDeletedRepositories(slug: string): Promise<APIResult<DeletedRepository[]>> {
  return request(`/api/v1/organizations/${encodeURIComponent(slug)}/deleted-repositories`);
}

export async function getTeams(slug: string): Promise<APIResult<Team[]>> {
  const result = await request<{ teams: Team[] }>(`/api/v1/organizations/${encodeURIComponent(slug)}/teams`);
  return result.ok ? { ok: true, data: result.data.teams } : result;
}

export function getTeam(
  organization: string,
  team: string,
): Promise<APIResult<{ team: Team; members?: TeamMember[] }>> {
  return request(`/api/v1/organizations/${encodeURIComponent(organization)}/teams/${encodeURIComponent(team)}`);
}

export async function getPublicRepositories(query = ""): Promise<APIResult<Repository[]>> {
  const search = new URLSearchParams({ limit: "100" });
  if (query.trim()) {
    search.set("q", query.trim());
  }
  const result = await request<{ repositories: Repository[] }>(`/api/v1/explore/repositories?${search.toString()}`);
  return result.ok ? { ok: true, data: result.data.repositories } : result;
}

export function getPublicRepository(owner: string, repository: string): Promise<APIResult<Repository>> {
  return request(`/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`);
}

export function getRepositorySettings(owner: string, repository: string): Promise<APIResult<Repository>> {
  return request(repositoryPath(owner, repository, "/settings"));
}

export async function getBranches(owner: string, repository: string): Promise<APIResult<Branch[]>> {
  const result = await getBranchOverview(owner, repository);
  return result.ok ? { ok: true, data: result.data.branches } : result;
}

export function getBranchOverview(owner: string, repository: string): Promise<APIResult<BranchOverview>> {
  return request(`/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/branches`);
}

export async function getBranchRules(owner: string, repository: string): Promise<APIResult<BranchRule[]>> {
  const result = await request<unknown>(repositoryPath(owner, repository, "/branch-rules"));
  if (!result.ok) return result;
  const rules = parseBranchRuleList(result.data);
  return rules ? { ok: true, data: rules } : { ok: false, reason: "unavailable" };
}

export async function getCIRuns(owner: string, repository: string): Promise<APIResult<CIRun[]>> {
  const result = await getActionRuns(owner, repository);
  return result.ok ? { ok: true, data: result.data.runs } : result;
}

export async function getActionWorkflows(owner: string, repository: string): Promise<APIResult<CIWorkflowPage>> {
  return request(repositoryPath(owner, repository, "/actions/workflows"));
}

export async function getActionRuns(owner: string, repository: string): Promise<APIResult<CIRunPage>> {
  return request(repositoryPath(owner, repository, "/actions/runs?per_page=50"));
}

export async function getDeployments(owner: string, repository: string): Promise<APIResult<Deployment[]>> {
  const result = await request<{ deployments: Deployment[] }>(
    repositoryPath(owner, repository, "/actions/deployments?limit=50"),
  );
  return result.ok ? { ok: true, data: result.data.deployments } : result;
}

export async function getActionsEnvironments(
  owner: string,
  repository: string,
): Promise<APIResult<ActionsEnvironment[]>> {
  const result = await request<{ environments: ActionsEnvironment[] }>(
    repositoryPath(owner, repository, "/actions/environments"),
  );
  return result.ok ? { ok: true, data: result.data.environments } : result;
}

export function getActionRun(owner: string, repository: string, runNumber: number): Promise<APIResult<CIRunDetail>> {
  return request(repositoryPath(owner, repository, `/actions/runs/${encodeURIComponent(String(runNumber))}`));
}

export async function getSARIFUploads(owner: string, repository: string): Promise<APIResult<SARIFUploadMetadata[]>> {
  const result = await request<{ sarifUploads: SARIFUploadMetadata[] }>(
    repositoryPath(owner, repository, "/code-scanning/sarif-uploads?limit=100"),
  );
  return result.ok ? { ok: true, data: result.data.sarifUploads } : result;
}

export async function getCodeScanningAlerts(
  owner: string,
  repository: string,
): Promise<APIResult<CodeScanningAlert[]>> {
  const result = await request<{ alerts: CodeScanningAlert[] }>(
    repositoryPath(owner, repository, "/code-scanning/alerts?per_page=1000"),
  );
  return result.ok ? { ok: true, data: result.data.alerts } : result;
}

export async function getIssues(
  owner: string,
  repository: string,
  query: RepositoryIssueQuery = {},
): Promise<APIResult<RepositoryIssuePage>> {
  const search = repositoryWorkItemSearchParams(query).toString();
  const result = await request<unknown>(repositoryPath(owner, repository, `/issues${search ? `?${search}` : ""}`));
  if (!result.ok) return result;
  const page = parseRepositoryIssuePage(result.data);
  return page ? { ok: true, data: page } : { ok: false, reason: "unavailable" };
}

export function getIssue(owner: string, repository: string, number: number): Promise<APIResult<Issue>> {
  return request(repositoryPath(owner, repository, `/issues/${number}`));
}

export type DiscussionState = "open" | "closed" | "all";
export type DiscussionSort = "newest" | "oldest" | "most-commented" | "most-voted";

export type DiscussionQuery = {
  category?: string;
  state?: DiscussionState;
  q?: string;
  sort?: DiscussionSort;
  page?: number;
  perPage?: number;
};

export function getDiscussionCategories(
  owner: string,
  repository: string,
): Promise<APIResult<DiscussionCategoriesPage>> {
  return request(repositoryPath(owner, repository, "/discussions/categories"));
}

export function getDiscussions(
  owner: string,
  repository: string,
  query: DiscussionQuery = {},
): Promise<APIResult<DiscussionPage>> {
  const search = queryString({
    category: query.category,
    state: query.state,
    q: query.q?.trim(),
    sort: query.sort,
    page: query.page ? String(query.page) : undefined,
    per_page: query.perPage ? String(query.perPage) : undefined,
  });
  return request(repositoryPath(owner, repository, `/discussions${search ? `?${search}` : ""}`));
}

export function getDiscussion(
  owner: string,
  repository: string,
  number: number,
  commentPage = 1,
): Promise<APIResult<Discussion>> {
  const search = queryString({ comment_page: String(commentPage) });
  return request(repositoryPath(owner, repository, `/discussions/${number}?${search}`));
}

export async function getIssueComments(
  owner: string,
  repository: string,
  number: number,
  page = 1,
): Promise<APIResult<CommentPage<IssueComment>>> {
  const search = commentPageSearch(page);
  const result = await request<unknown>(repositoryPath(owner, repository, `/issues/${number}/comments?${search}`));
  if (!result.ok) return result;
  const comments = parseIssueCommentPage(result.data, page, conversationCommentPageSize);
  return comments ? { ok: true, data: comments } : { ok: false, reason: "unavailable" };
}

export async function getLabels(owner: string, repository: string): Promise<APIResult<Label[]>> {
  const result = await getLabelPage(owner, repository);
  return result.ok ? { ok: true, data: result.data.items } : result;
}

export function getLabelPage(owner: string, repository: string): Promise<APIResult<LabelPage>> {
  return request(repositoryPath(owner, repository, "/labels?limit=100"));
}

export async function getMergeRequests(
  owner: string,
  repository: string,
  query: RepositoryMergeRequestQuery = {},
): Promise<APIResult<RepositoryMergeRequestPage>> {
  const search = repositoryWorkItemSearchParams(query).toString();
  const result = await request<unknown>(
    repositoryPath(owner, repository, `/merge-requests${search ? `?${search}` : ""}`),
  );
  if (!result.ok) return result;
  const page = parseRepositoryMergeRequestPage(result.data);
  return page ? { ok: true, data: page } : { ok: false, reason: "unavailable" };
}

export function getMergeRequest(owner: string, repository: string, number: number): Promise<APIResult<MergeRequest>> {
  return request(repositoryPath(owner, repository, `/merge-requests/${number}`));
}

export async function getMergeRequestComments(
  owner: string,
  repository: string,
  number: number,
  page = 1,
): Promise<APIResult<CommentPage<MergeRequestComment>>> {
  const search = commentPageSearch(page);
  const result = await request<unknown>(
    repositoryPath(owner, repository, `/merge-requests/${number}/comments?${search}`),
  );
  if (!result.ok) return result;
  const comments = parseMergeRequestCommentPage(result.data, page, conversationCommentPageSize);
  return comments ? { ok: true, data: comments } : { ok: false, reason: "unavailable" };
}

export function getProjects(owner: string, repository: string): Promise<APIResult<ProjectList>> {
  return request(repositoryPath(owner, repository, "/projects"));
}

export function getProject(owner: string, repository: string, number: number): Promise<APIResult<Project>> {
  return request(repositoryPath(owner, repository, `/projects/${number}`));
}

export function getWikiPages(owner: string, repository: string, query = ""): Promise<APIResult<WikiPageList>> {
  const search = query.trim() ? `?${new URLSearchParams({ q: query.trim() }).toString()}` : "";
  return request(repositoryPath(owner, repository, `/wiki${search}`));
}

export function getWikiPage(owner: string, repository: string, slug: string): Promise<APIResult<WikiPage>> {
  return request(repositoryPath(owner, repository, `/wiki/${encodeURIComponent(slug)}`));
}

export async function getWikiHistory(
  owner: string,
  repository: string,
  slug: string,
): Promise<APIResult<WikiRevision[]>> {
  const result = await request<{ revisions: WikiRevision[] }>(
    repositoryPath(owner, repository, `/wiki/${encodeURIComponent(slug)}/history`),
  );
  return result.ok ? { ok: true, data: result.data.revisions } : result;
}

export function getWikiRevision(
  owner: string,
  repository: string,
  slug: string,
  version: number,
): Promise<APIResult<WikiRevision>> {
  return request(repositoryPath(owner, repository, `/wiki/${encodeURIComponent(slug)}/history/${version}`));
}

export function getReleases(owner: string, repository: string, page: number): Promise<APIResult<ReleasePage>> {
  const query = new URLSearchParams({ page: String(page), perPage: "20" });
  return request(repositoryPath(owner, repository, `/releases?${query.toString()}`));
}

export function getMilestones(
  owner: string,
  repository: string,
  state: "open" | "closed" | "all",
  page = 1,
  perPage = 20,
): Promise<APIResult<MilestonePage>> {
  const query = new URLSearchParams({ state, page: String(page), perPage: String(perPage) });
  return request(repositoryPath(owner, repository, `/milestones?${query.toString()}`));
}

export function getAssignableUsers(
  owner: string,
  repository: string,
  query = "",
  limit = 100,
): Promise<APIResult<AssigneePage>> {
  const parameters = new URLSearchParams({ limit: String(limit) });
  if (query) parameters.set("query", query);
  return request(repositoryPath(owner, repository, `/assignees?${parameters.toString()}`));
}

export function getReviews(owner: string, repository: string, number: number): Promise<APIResult<ReviewSummary>> {
  return request(repositoryPath(owner, repository, `/merge-requests/${number}/reviews`));
}

export function getMergeRequestMetadata(
  owner: string,
  repository: string,
  number: number,
): Promise<APIResult<MergeRequestMetadata>> {
  return request(repositoryPath(owner, repository, `/merge-requests/${number}/metadata`));
}

export function getReviewRequests(
  owner: string,
  repository: string,
  number: number,
): Promise<APIResult<ReviewRequestSummary>> {
  return request(repositoryPath(owner, repository, `/merge-requests/${number}/review-requests`));
}

export async function getReviewCandidates(
  owner: string,
  repository: string,
  number: number,
): Promise<APIResult<ReviewCandidate[]>> {
  const result = await request<{ items: ReviewCandidate[] }>(
    repositoryPath(owner, repository, `/merge-requests/${number}/review-candidates`),
  );
  return result.ok ? { ok: true, data: result.data.items } : result;
}

export async function getReviewThreads(
  owner: string,
  repository: string,
  number: number,
): Promise<APIResult<ReviewThread[]>> {
  const result = await request<{ threads: ReviewThread[] }>(
    repositoryPath(owner, repository, `/merge-requests/${number}/review-threads`),
  );
  return result.ok ? { ok: true, data: result.data.threads } : result;
}

export async function getMergeReadiness(
  owner: string,
  repository: string,
  number: number,
): Promise<APIResult<MergeReadiness>> {
  const result = await request<unknown>(repositoryPath(owner, repository, `/merge-requests/${number}/merge-readiness`));
  if (!result.ok) return result;
  const readiness = parseMergeReadiness(result.data);
  return readiness ? { ok: true, data: readiness } : { ok: false, reason: "unavailable" };
}

export function getMergeOperation(
  owner: string,
  repository: string,
  number: number,
): Promise<APIResult<MergeOperation>> {
  return request(repositoryPath(owner, repository, `/merge-requests/${number}/merge-operation`));
}

export function getLoreTree(
  owner: string,
  repository: string,
  query: { branch?: string; revision?: string; path?: string },
): Promise<APIResult<LoreTree>> {
  return request(repositoryPath(owner, repository, `/tree?${queryString(query)}`));
}

export function getLoreFile(
  owner: string,
  repository: string,
  query: { branch?: string; revision?: string; path: string },
): Promise<APIResult<LoreFile>> {
  return request(repositoryPath(owner, repository, `/file?${queryString(query)}`));
}

export function getRevisionHistory(
  owner: string,
  repository: string,
  query: { branch?: string; revision?: string },
): Promise<APIResult<{ revision: string; entries: RevisionHistoryEntry[]; hasMore: boolean }>> {
  return request(repositoryPath(owner, repository, `/revisions?${queryString(query)}`));
}

export function getFileHistory(
  owner: string,
  repository: string,
  query: { branch?: string; revision?: string; path: string },
): Promise<APIResult<{ revision: string; path: string; entries: FileHistoryEntry[]; hasMore: boolean }>> {
  return request(repositoryPath(owner, repository, `/file/history?${queryString(query)}`));
}

export function getRevision(owner: string, repository: string, revision: string): Promise<APIResult<LoreRevision>> {
  return request(repositoryPath(owner, repository, `/revisions/${encodeURIComponent(revision)}`));
}

export async function getRevisionStatuses(
  owner: string,
  repository: string,
  revision: string,
): Promise<APIResult<RevisionStatusPage>> {
  const result = await request<unknown>(
    repositoryPath(owner, repository, `/revisions/${encodeURIComponent(revision)}/statuses`),
  );
  return parseRevisionStatusResponse(result);
}

export function getLoreDiff(
  owner: string,
  repository: string,
  source: string,
  target: string,
): Promise<APIResult<LoreDiff>> {
  const query = new URLSearchParams({ source, target });
  return request(repositoryPath(owner, repository, `/diff?${query.toString()}`));
}

function repositoryPath(owner: string, repository: string, suffix: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}${suffix}`;
}

function commentPageSearch(page: number): string {
  return queryString({
    limit: String(conversationCommentPageSize),
    cursor: page > 1 ? String((page - 1) * conversationCommentPageSize) : undefined,
  });
}

async function request<T>(path: string): Promise<APIResult<T>> {
  const signal = AbortSignal.timeout(4_000);
  try {
    const cookieHeader = (await cookies()).toString();
    const headers = new Headers({ Accept: "application/json" });
    if (cookieHeader) {
      headers.set("Cookie", cookieHeader);
    }
    const response = await fetch(new URL(path, apiOrigin), {
      cache: "no-store",
      headers,
      signal,
    });
    if (response.status === 404) {
      return { ok: false, reason: "not-found", code: await readProblemCode(response) };
    }
    if (response.status === 401) {
      return { ok: false, reason: "unauthorized", code: await readProblemCode(response) };
    }
    if (response.status === 403) {
      return { ok: false, reason: "forbidden", code: await readProblemCode(response) };
    }
    if (response.status >= 400 && response.status < 500) {
      return { ok: false, reason: "invalid", code: await readProblemCode(response) };
    }
    if (!response.ok) {
      return { ok: false, reason: "unavailable" };
    }
    return { ok: true, data: (await response.json()) as T };
  } catch {
    return { ok: false, reason: "unavailable" };
  }
}

function queryString(query: Record<string, string | undefined>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value) {
      params.set(key, value);
    }
  }
  return params.toString();
}

async function readProblemCode(response: Response): Promise<string | undefined> {
  try {
    const payload = (await response.json()) as {
      error?: { code?: unknown };
      code?: unknown;
      type?: unknown;
    };
    if (typeof payload.error?.code === "string") return payload.error.code;
    if (typeof payload.code === "string") return payload.code;
    if (typeof payload.type === "string") {
      const marker = "/problems/";
      const index = payload.type.lastIndexOf(marker);
      return index >= 0 ? payload.type.slice(index + marker.length) : undefined;
    }
    return undefined;
  } catch {
    return undefined;
  }
}
