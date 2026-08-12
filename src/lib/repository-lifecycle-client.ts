"use client";

import type { Repository } from "./api-types";
import { deleteJsonWithBody, putJson } from "./auth-client";

function archivePath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/archive`;
}

export function archiveRepository(owner: string, repository: string, confirmation: string, csrfToken: string) {
  return putJson<Repository>(archivePath(owner, repository), { confirmation }, csrfToken);
}

export function unarchiveRepository(owner: string, repository: string, confirmation: string, csrfToken: string) {
  return deleteJsonWithBody<Repository>(archivePath(owner, repository), { confirmation }, csrfToken);
}
