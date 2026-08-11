import "server-only";

import { cookies } from "next/headers";

import type {
  APIResult,
  AssigneePage,
  Branch,
  BranchOverview,
  BranchRule,
  CIRun,
  CIRunDetail,
  CIRunPage,
  CIWorkflowPage,
  CodeScanningAlert,
  DashboardData,
  FileHistoryEntry,
  Issue,
  IssueComment,
  Label,
  LoreDiff,
  LoreFile,
  LoreRevision,
  LoreTree,
  MergeOperation,
  MergeReadiness,
  MergeRequest,
  MergeRequestComment,
  MilestonePage,
  Notification,
  NotificationPage,
  NotificationPreferences,
  OrganizationView,
  Project,
  ProjectList,
  ReleasePage,
  Repository,
  ReviewSummary,
  RevisionHistoryEntry,
  SARIFUploadMetadata,
  SearchResults,
  Team,
  TeamMember,
  UserProfile,
} from "./api-types";

const apiOrigin = process.env.LOREHUB_API_URL ?? "http://127.0.0.1:8080";

export function getDashboard(): Promise<APIResult<DashboardData>> {
  return request("/api/v1/dashboard");
}

export function getSearchResults(query: string): Promise<APIResult<SearchResults>> {
  const params = new URLSearchParams({ q: query.trim(), limit: "50" });
  return request(`/api/v1/search?${params.toString()}`);
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

export function getOrganization(slug: string): Promise<APIResult<OrganizationView>> {
  return request(`/api/v1/organizations/${encodeURIComponent(slug)}`);
}

export async function getOrganizationRepositories(slug: string): Promise<APIResult<Repository[]>> {
  const result = await request<{ repositories: Repository[] }>(
    `/api/v1/organizations/${encodeURIComponent(slug)}/repositories`,
  );
  return result.ok ? { ok: true, data: result.data.repositories } : result;
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
  const result = await request<{ items: BranchRule[] }>(repositoryPath(owner, repository, "/branch-rules"));
  return result.ok ? { ok: true, data: result.data.items } : result;
}

export async function getOpenIssues(owner: string, repository: string): Promise<APIResult<Issue[]>> {
  return getIssues(owner, repository, "open");
}

export async function getOpenMergeRequests(owner: string, repository: string): Promise<APIResult<MergeRequest[]>> {
  return getMergeRequests(owner, repository, "open");
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

export type IssueFilter = "open" | "closed" | "all";
export type MergeRequestFilter = "open" | "closed" | "merged" | "all";

export async function getIssues(owner: string, repository: string, state: IssueFilter): Promise<APIResult<Issue[]>> {
  if (state === "all") {
    return combineResults(
      await Promise.all([getIssues(owner, repository, "open"), getIssues(owner, repository, "closed")]),
      (items) => items.sort((left, right) => Date.parse(right.updatedAt) - Date.parse(left.updatedAt)),
    );
  }
  const result = await request<{ issues: Issue[] }>(repositoryPath(owner, repository, `/issues?state=${state}`));
  return result.ok ? { ok: true, data: result.data.issues } : result;
}

export function getIssue(owner: string, repository: string, number: number): Promise<APIResult<Issue>> {
  return request(repositoryPath(owner, repository, `/issues/${number}`));
}

export async function getIssueComments(
  owner: string,
  repository: string,
  number: number,
): Promise<APIResult<IssueComment[]>> {
  const result = await request<{ items: IssueComment[] }>(
    repositoryPath(owner, repository, `/issues/${number}/comments?limit=100`),
  );
  return result.ok ? { ok: true, data: result.data.items } : result;
}

export async function getLabels(owner: string, repository: string): Promise<APIResult<Label[]>> {
  const result = await request<{ items: Label[] }>(repositoryPath(owner, repository, "/labels?limit=100"));
  return result.ok ? { ok: true, data: result.data.items } : result;
}

export async function getMergeRequests(
  owner: string,
  repository: string,
  state: MergeRequestFilter,
): Promise<APIResult<MergeRequest[]>> {
  if (state === "all") {
    return combineResults(
      await Promise.all([
        getMergeRequests(owner, repository, "open"),
        getMergeRequests(owner, repository, "closed"),
        getMergeRequests(owner, repository, "merged"),
      ]),
      (items) => items.sort((left, right) => Date.parse(right.updatedAt) - Date.parse(left.updatedAt)),
    );
  }
  const result = await request<{ mergeRequests: MergeRequest[] }>(
    repositoryPath(owner, repository, `/merge-requests?state=${state}`),
  );
  return result.ok ? { ok: true, data: result.data.mergeRequests } : result;
}

export function getMergeRequest(owner: string, repository: string, number: number): Promise<APIResult<MergeRequest>> {
  return request(repositoryPath(owner, repository, `/merge-requests/${number}`));
}

export async function getMergeRequestComments(
  owner: string,
  repository: string,
  number: number,
): Promise<APIResult<MergeRequestComment[]>> {
  const result = await request<{ items: MergeRequestComment[] }>(
    repositoryPath(owner, repository, `/merge-requests/${number}/comments?limit=100`),
  );
  return result.ok ? { ok: true, data: result.data.items } : result;
}

export function getProjects(owner: string, repository: string): Promise<APIResult<ProjectList>> {
  return request(repositoryPath(owner, repository, "/projects"));
}

export function getProject(owner: string, repository: string, number: number): Promise<APIResult<Project>> {
  return request(repositoryPath(owner, repository, `/projects/${number}`));
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

export function getMergeReadiness(
  owner: string,
  repository: string,
  number: number,
): Promise<APIResult<MergeReadiness>> {
  return request(repositoryPath(owner, repository, `/merge-requests/${number}/merge-readiness`));
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

function combineResults<T>(results: APIResult<T[]>[], combine: (items: T[]) => T[]): APIResult<T[]> {
  const failure = results.find((result) => !result.ok);
  if (failure && !failure.ok) {
    return failure;
  }
  return { ok: true, data: combine(results.flatMap((result) => (result.ok ? result.data : []))) };
}
