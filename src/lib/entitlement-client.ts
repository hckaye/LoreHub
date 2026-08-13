"use client";

import { deleteJsonWithBody, postJson, type MutationResult } from "./auth-client";
import {
  entitlementInput,
  entitlementsPath,
  normalizeEntitlement,
  type Entitlement,
  type EntitlementFeature,
  type EntitlementSubject,
} from "./entitlements";

export async function grantEntitlement(
  subject: EntitlementSubject,
  feature: EntitlementFeature,
  csrfToken: string,
): Promise<MutationResult<Entitlement>> {
  const result = await postJson<unknown>(entitlementsPath, entitlementInput(subject, feature), csrfToken);
  if (!result.ok) return result;
  const entitlement = normalizeEntitlement(result.data);
  return entitlement ? { ok: true, data: entitlement } : { ok: false, kind: "unavailable", code: "invalid_response" };
}

export function revokeEntitlement(
  subject: EntitlementSubject,
  feature: EntitlementFeature,
  csrfToken: string,
): Promise<MutationResult<null>> {
  return deleteJsonWithBody(entitlementsPath, entitlementInput(subject, feature), csrfToken);
}
