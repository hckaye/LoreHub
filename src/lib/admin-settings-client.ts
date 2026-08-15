"use client";

import {
  adminSettingsInput,
  adminSettingsPath,
  normalizeAdminSettings,
  type AdminSettings,
  type AdminSettingsInput,
} from "./admin-settings";
import { putJson, type MutationResult } from "./auth-client";

export async function updateAdminSettings(
  input: AdminSettingsInput,
  csrfToken: string,
): Promise<MutationResult<AdminSettings>> {
  const result = await putJson<unknown>(adminSettingsPath, adminSettingsInput(input), csrfToken);
  if (!result.ok) return result;
  const settings = normalizeAdminSettings(result.data);
  return settings ? { ok: true, data: settings } : { ok: false, kind: "unavailable", code: "invalid_response" };
}
