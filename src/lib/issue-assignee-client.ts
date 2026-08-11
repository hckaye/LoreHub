"use client";

import type { APIResult, Assignee, AssigneePage } from "./api-types";
import { deleteJson, putJson } from "./auth-client";

export async function searchIssueAssignees(
  owner: string,
  repository: string,
  query: string,
  signal?: AbortSignal,
): Promise<APIResult<AssigneePage>> {
  const parameters = new URLSearchParams({ limit: "100", query });
  try {
    const response = await fetch(`${repositoryPath(owner, repository)}/assignees?${parameters.toString()}`, {
      credentials: "include",
      headers: { Accept: "application/json" },
      signal,
    });
    if (response.ok) return { ok: true, data: (await response.json()) as AssigneePage };
    if (response.status === 401) return { ok: false, reason: "unauthorized" };
    if (response.status === 403) return { ok: false, reason: "forbidden" };
    if (response.status === 404) return { ok: false, reason: "not-found" };
    return { ok: false, reason: response.status < 500 ? "invalid" : "unavailable" };
  } catch {
    return { ok: false, reason: "unavailable" };
  }
}

export function assignIssueUser(
  owner: string,
  repository: string,
  issueNumber: number,
  username: string,
  csrfToken: string,
) {
  return putJson<Assignee>(assigneePath(owner, repository, issueNumber, username), undefined, csrfToken);
}

export function removeIssueUser(
  owner: string,
  repository: string,
  issueNumber: number,
  username: string,
  csrfToken: string,
) {
  return deleteJson<null>(assigneePath(owner, repository, issueNumber, username), csrfToken);
}

function assigneePath(owner: string, repository: string, issueNumber: number, username: string): string {
  return `${repositoryPath(owner, repository)}/issues/${issueNumber}/assignees/${encodeURIComponent(username)}`;
}

function repositoryPath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`;
}
