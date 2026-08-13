"use client";

import { deleteJson, postJson, putJson, type MutationResult } from "./auth-client";
import {
  defaultLoreServerPath,
  loreServerPath,
  loreServerRegistrationTokenPath,
  normalizeDefaultLoreServer,
  normalizeLoreServerRegistration,
  type LoreServer,
  type LoreServerRegistration,
} from "./lore-servers";

export async function createLoreServerRegistrationToken(
  organization: string,
  csrfToken: string,
): Promise<MutationResult<LoreServerRegistration>> {
  const result = await postJson<unknown>(loreServerRegistrationTokenPath(organization), {}, csrfToken);
  if (!result.ok) return result;
  const registration = normalizeLoreServerRegistration(result.data);
  return registration ? { ok: true, data: registration } : { ok: false, kind: "unavailable", code: "invalid_response" };
}

export function revokeLoreServer(
  organization: string,
  loreServerID: string,
  csrfToken: string,
): Promise<MutationResult<null>> {
  return deleteJson(loreServerPath(organization, loreServerID), csrfToken);
}

export async function setDefaultLoreServer(
  organization: string,
  loreServerID: string | null,
  csrfToken: string,
): Promise<MutationResult<LoreServer | null>> {
  const result = await putJson<unknown>(defaultLoreServerPath(organization), { loreServerId: loreServerID }, csrfToken);
  if (!result.ok) return result;
  return { ok: true, data: normalizeDefaultLoreServer(result.data) };
}
