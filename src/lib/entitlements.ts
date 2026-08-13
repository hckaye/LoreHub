export const entitlementFeatures = ["hosted_runners", "hosted_lore_server"] as const;

export type EntitlementFeature = (typeof entitlementFeatures)[number];

export type Entitlement = {
  organizationId: string | null;
  userId: string | null;
  feature: string;
  grantedBy: string | null;
  grantSource: string;
  createdAt: string;
  revokedAt: string | null;
};

export type EntitlementSubject = { kind: "organization" | "user"; id: string };

export type EntitlementInput = {
  organizationId?: string;
  userId?: string;
  feature: EntitlementFeature;
};

export const entitlementsPath = "/api/v1/admin/entitlements";

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isEntitlementSubjectID(value: string): boolean {
  return uuidPattern.test(value.trim());
}

export function entitlementInput(subject: EntitlementSubject, feature: EntitlementFeature): EntitlementInput {
  const id = subject.id.trim();
  return subject.kind === "organization" ? { organizationId: id, feature } : { userId: id, feature };
}

export function entitlementKey(entitlement: Entitlement): string {
  return `${entitlement.organizationId ?? entitlement.userId ?? ""}:${entitlement.feature}`;
}

export function isEntitlementFeature(value: string): value is EntitlementFeature {
  return (entitlementFeatures as readonly string[]).includes(value);
}

export function normalizeEntitlementList(value: unknown): Entitlement[] | null {
  if (!isRecord(value) || !Array.isArray(value.entitlements)) return null;
  const entitlements = value.entitlements.map(normalizeEntitlement);
  return entitlements.some((entitlement) => entitlement === null) ? null : (entitlements as Entitlement[]);
}

export function normalizeEntitlement(value: unknown): Entitlement | null {
  if (!isRecord(value) || typeof value.feature !== "string" || typeof value.createdAt !== "string") return null;
  const organizationId = nullableString(value.organizationId);
  const userId = nullableString(value.userId);
  if (organizationId === null && userId === null) return null;
  return {
    organizationId,
    userId,
    feature: value.feature,
    grantedBy: nullableString(value.grantedBy),
    grantSource: typeof value.grantSource === "string" ? value.grantSource : "",
    createdAt: value.createdAt,
    revokedAt: nullableString(value.revokedAt),
  };
}

function nullableString(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
