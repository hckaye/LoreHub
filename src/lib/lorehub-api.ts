import "server-only";

import type { APIResult, Branch, CIRun, Issue, MergeRequest, Repository } from "./api-types";

const apiOrigin = process.env.LOREHUB_API_URL ?? "http://127.0.0.1:8080";

export async function getPublicRepositories(): Promise<APIResult<Repository[]>> {
  const result = await request<{ repositories: Repository[] }>("/api/v1/explore/repositories?limit=30");
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
  const result = await request<{ issues: Issue[] }>(
    `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/issues?state=open`,
  );
  return result.ok ? { ok: true, data: result.data.issues } : result;
}

export async function getOpenMergeRequests(owner: string, repository: string): Promise<APIResult<MergeRequest[]>> {
  const result = await request<{ mergeRequests: MergeRequest[] }>(
    repositoryPath(owner, repository, "/merge-requests?state=open"),
  );
  return result.ok ? { ok: true, data: result.data.mergeRequests } : result;
}

export async function getCIRuns(owner: string, repository: string): Promise<APIResult<CIRun[]>> {
  const result = await request<{ runs: CIRun[] }>(repositoryPath(owner, repository, "/actions/runs"));
  return result.ok ? { ok: true, data: result.data.runs } : result;
}

function repositoryPath(owner: string, repository: string, suffix: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}${suffix}`;
}

async function request<T>(path: string): Promise<APIResult<T>> {
  const signal = AbortSignal.timeout(4_000);
  try {
    const response = await fetch(new URL(path, apiOrigin), {
      cache: "no-store",
      headers: { Accept: "application/json" },
      signal,
    });
    if (response.status === 404) {
      return { ok: false, reason: "not-found" };
    }
    if (!response.ok) {
      return { ok: false, reason: "unavailable" };
    }
    return { ok: true, data: (await response.json()) as T };
  } catch {
    return { ok: false, reason: "unavailable" };
  }
}
