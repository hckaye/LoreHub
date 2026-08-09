import "server-only";

import { cookies } from "next/headers";

import type {
  APIResult,
  Branch,
  CIRun,
  CIRunDetail,
  CIRunPage,
  CIWorkflowPage,
  DashboardData,
  FileHistoryEntry,
  Issue,
  LoreDiff,
  LoreFile,
  LoreRevision,
  LoreTree,
  MergeOperation,
  MergeReadiness,
  MergeRequest,
  Notification,
  NotificationPreferences,
  OrganizationView,
  Repository,
  ReviewSummary,
  RevisionHistoryEntry,
  SearchResults,
  Team,
  TeamMember,
  UserProfile,
} from "./api-types";

export async function getDashboard(): Promise<APIResult<DashboardData>> {
  return request<DashboardData>("/api/v1/dashboard");
}

export async function getSearchResults(query: string): Promise<APIResult<SearchResults>> {
  const params = new URLSearchParams({ q: query, type: "all", limit: "30" });
  return request<SearchResults>(`/api/v1/search?${params.toString()}`);
}

export async function getUserProfile(username: string): Promise<APIResult<UserProfile>> {
  return request<UserProfile>(`/api/v1/users/${encodeURIComponent(username)}`);
}

export async function getUserRepositories(username: string): Promise<APIResult<Repository[]>> {
  const result = await request<{ repositories: Repository[] }>(
    `/api/v1/users/${encodeURIComponent(username)}/repositories`,
  );
  return result.ok ? { ok: true, data: result.data.repositories } : result;
}

export async function getOrganization(slug: string): Promise<APIResult<OrganizationView>> {
  return request<OrganizationView>(`/api/v1/organizations/${encodeURIComponent(slug)}`);
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

export async function getTeam(slug: string, team: string): Promise<APIResult<{ team: Team; members?: TeamMember[] }>> {
  return request<{ team: Team; members?: TeamMember[] }>(
    `/api/v1/organizations/${encodeURIComponent(slug)}/teams/${encodeURIComponent(team)}`,
  );
}

export async function getNotifications(unreadOnly = false): Promise<APIResult<Notification[]>> {
  const result = await request<{ items: Notification[] }>(
    `/api/v1/notifications?unread=${String(unreadOnly)}&limit=100`,
  );
  return result.ok ? { ok: true, data: result.data.items } : result;
}

export async function getNotificationPreferences(): Promise<APIResult<NotificationPreferences>> {
  return request<NotificationPreferences>("/api/v1/account/notification-preferences");
}

const apiOrigin = process.env.LOREHUB_API_URL ?? "http://127.0.0.1:8080";

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
  return request(`/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/settings`);
}

export async function getBranches(owner: string, repository: string): Promise<APIResult<Branch[]>> {
  const result = await request<{ branches: Branch[] }>(
    `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/branches`,
  );
  return result.ok ? { ok: true, data: result.data.branches } : result;
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
    const payload = (await response.json()) as { error?: { code?: unknown } };
    return typeof payload.error?.code === "string" ? payload.error.code : undefined;
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
