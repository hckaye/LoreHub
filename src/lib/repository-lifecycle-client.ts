"use client";

import type { DeletedRepository, Repository } from "./api-types";
import { deleteJsonWithBody, postJson, putJson } from "./auth-client";

function archivePath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/archive`;
}

export function archiveRepository(owner: string, repository: string, confirmation: string, csrfToken: string) {
  return putJson<Repository>(archivePath(owner, repository), { confirmation }, csrfToken);
}

export function unarchiveRepository(owner: string, repository: string, confirmation: string, csrfToken: string) {
  return deleteJsonWithBody<Repository>(archivePath(owner, repository), { confirmation }, csrfToken);
}

export function scheduleRepositoryDeletion(owner: string, repository: string, confirmation: string, csrfToken: string) {
  const path = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`;
  return deleteJsonWithBody<DeletedRepository>(path, { confirmation }, csrfToken);
}

export function restoreDeletedRepository(owner: string, repository: string, csrfToken: string) {
  const path =
    `/api/v1/organizations/${encodeURIComponent(owner)}/deleted-repositories/` +
    `${encodeURIComponent(repository)}/restore`;
  return postJson<Repository>(path, {}, csrfToken);
}
