"use client";

import type { CreatedPersonalAccessToken, PersonalAccessTokenScope } from "./api-types";
import { deleteJson, postJson, type MutationResult } from "./auth-client";
import { normalizeCreatedPersonalAccessToken } from "./personal-access-token";

export type CreatePersonalAccessTokenInput = {
  name: string;
  scopes: PersonalAccessTokenScope[];
  expiresAt: string;
};

export async function createPersonalAccessToken(
  input: CreatePersonalAccessTokenInput,
  csrfToken: string,
): Promise<MutationResult<CreatedPersonalAccessToken>> {
  const result = await postJson<unknown>("/api/v1/account/personal-access-tokens", input, csrfToken);
  if (!result.ok) return result;
  const normalized = normalizeCreatedPersonalAccessToken(result.data);
  return normalized ? { ok: true, data: normalized } : { ok: false, kind: "unavailable", code: "invalid_response" };
}

export function revokePersonalAccessToken(tokenID: string, csrfToken: string): Promise<MutationResult<null>> {
  return deleteJson(`/api/v1/account/personal-access-tokens/${encodeURIComponent(tokenID)}`, csrfToken);
}
