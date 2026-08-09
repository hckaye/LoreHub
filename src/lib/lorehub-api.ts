import "server-only";

import { cookies } from "next/headers";

import type {
  APIResult,
  Branch,
  CIRun,
  CIRunDetail,
  CIRunPage,
  CIWorkflowPage,
  Issue,
  MergeRequest,
  Repository,
} from "./api-types";

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
      return { ok: false, reason: "not-found" };
    }
    if (response.status === 401) {
      return { ok: false, reason: "unauthorized" };
    }
    if (response.status === 403) {
      return { ok: false, reason: "forbidden" };
    }
    if (response.status >= 400 && response.status < 500) {
      return { ok: false, reason: "invalid" };
    }
    if (!response.ok) {
      return { ok: false, reason: "unavailable" };
    }
    return { ok: true, data: (await response.json()) as T };
  } catch {
    return { ok: false, reason: "unavailable" };
  }
}

function combineResults<T>(results: APIResult<T[]>[], combine: (items: T[]) => T[]): APIResult<T[]> {
  const failure = results.find((result) => !result.ok);
  if (failure && !failure.ok) {
    return failure;
  }
  return { ok: true, data: combine(results.flatMap((result) => (result.ok ? result.data : []))) };
}
