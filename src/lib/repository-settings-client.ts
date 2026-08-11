"use client";

import type { Repository } from "./api-types";
import { patchJson } from "./auth-client";

export type RepositorySettingsInput = Pick<Repository, "displayName" | "description" | "homepageUrl" | "visibility">;

export function updateRepositorySettings(
  owner: string,
  repository: string,
  input: RepositorySettingsInput,
  csrfToken: string,
) {
  const path = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/settings`;
  return patchJson<Repository>(path, input, csrfToken);
}
