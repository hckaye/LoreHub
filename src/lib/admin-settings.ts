export type AdminSettings = {
  hostedLoreServerEnabled: boolean;
  hostedLoreServerOverride: boolean | null;
  hostedLoreServerDefault: boolean;
  maxOrganizationsPerUser: number;
  maxOrganizationsPerUserOverride: number | null;
  maxOrganizationsPerUserDefault: number;
  maxRepositoriesPerOrganization: number;
  maxRepositoriesPerOrganizationOverride: number | null;
  maxRepositoriesPerOrganizationDefault: number;
  maxRepositorySizeBytes: number;
  maxRepositorySizeBytesOverride: number | null;
  maxRepositorySizeBytesDefault: number;
};

export type AdminSettingsInput = {
  hostedLoreServerOverride?: boolean | null;
  maxOrganizationsPerUserOverride?: number | null;
  maxRepositoriesPerOrganizationOverride?: number | null;
  maxRepositorySizeBytesOverride?: number | null;
};

export const hostedLoreServerChoices = ["default", "enabled", "disabled"] as const;

export type HostedLoreServerChoice = (typeof hostedLoreServerChoices)[number];

export const adminSettingsPath = "/api/v1/admin/settings";

export function adminSettingsInput(input: AdminSettingsInput): AdminSettingsInput {
  return { ...input };
}

export function hostedLoreServerChoice(override: boolean | null): HostedLoreServerChoice {
  if (override === true) return "enabled";
  if (override === false) return "disabled";
  return "default";
}

export function hostedLoreServerOverride(choice: HostedLoreServerChoice): boolean | null {
  if (choice === "enabled") return true;
  if (choice === "disabled") return false;
  return null;
}

export function isHostedLoreServerChoice(value: string): value is HostedLoreServerChoice {
  return (hostedLoreServerChoices as readonly string[]).includes(value);
}

export function overrideInputValue(override: number | null): string {
  return override === null ? "" : String(override);
}

export function parseOverrideInput(value: string): number | null {
  const trimmed = value.trim();
  if (trimmed === "") return null;
  return Number(trimmed);
}

export function normalizeAdminSettings(value: unknown): AdminSettings | null {
  if (!isRecord(value)) return null;
  if (typeof value.hostedLoreServerEnabled !== "boolean") return null;
  if (typeof value.hostedLoreServerDefault !== "boolean") return null;
  if (value.hostedLoreServerOverride !== null && typeof value.hostedLoreServerOverride !== "boolean") return null;
  if (!isInt(value.maxOrganizationsPerUser) || !isInt(value.maxOrganizationsPerUserDefault)) return null;
  if (!isNullableInt(value.maxOrganizationsPerUserOverride)) return null;
  if (!isInt(value.maxRepositoriesPerOrganization) || !isInt(value.maxRepositoriesPerOrganizationDefault)) return null;
  if (!isNullableInt(value.maxRepositoriesPerOrganizationOverride)) return null;
  if (!isInt(value.maxRepositorySizeBytes) || !isInt(value.maxRepositorySizeBytesDefault)) return null;
  if (!isNullableInt(value.maxRepositorySizeBytesOverride)) return null;
  return {
    hostedLoreServerEnabled: value.hostedLoreServerEnabled,
    hostedLoreServerOverride: value.hostedLoreServerOverride,
    hostedLoreServerDefault: value.hostedLoreServerDefault,
    maxOrganizationsPerUser: value.maxOrganizationsPerUser,
    maxOrganizationsPerUserOverride: value.maxOrganizationsPerUserOverride,
    maxOrganizationsPerUserDefault: value.maxOrganizationsPerUserDefault,
    maxRepositoriesPerOrganization: value.maxRepositoriesPerOrganization,
    maxRepositoriesPerOrganizationOverride: value.maxRepositoriesPerOrganizationOverride,
    maxRepositoriesPerOrganizationDefault: value.maxRepositoriesPerOrganizationDefault,
    maxRepositorySizeBytes: value.maxRepositorySizeBytes,
    maxRepositorySizeBytesOverride: value.maxRepositorySizeBytesOverride,
    maxRepositorySizeBytesDefault: value.maxRepositorySizeBytesDefault,
  };
}

function isInt(value: unknown): value is number {
  return typeof value === "number" && Number.isInteger(value);
}

function isNullableInt(value: unknown): value is number | null {
  return value === null || isInt(value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
