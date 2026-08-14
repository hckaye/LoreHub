"use client";

import { adminSettingsInput, adminSettingsPath, normalizeAdminSettings, type AdminSettings } from "./admin-settings";
import { putJson, type MutationResult } from "./auth-client";

export async function updateAdminSettings(
  override: boolean | null,
  csrfToken: string,
): Promise<MutationResult<AdminSettings>> {
  const result = await putJson<unknown>(adminSettingsPath, adminSettingsInput(override), csrfToken);
  if (!result.ok) return result;
  const settings = normalizeAdminSettings(result.data);
  return settings ? { ok: true, data: settings } : { ok: false, kind: "unavailable", code: "invalid_response" };
}
