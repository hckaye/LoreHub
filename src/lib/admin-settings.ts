export type AdminSettings = {
  hostedLoreServerEnabled: boolean;
  hostedLoreServerOverride: boolean | null;
  hostedLoreServerDefault: boolean;
};

export type AdminSettingsInput = {
  hostedLoreServerOverride: boolean | null;
};

export const hostedLoreServerChoices = ["default", "enabled", "disabled"] as const;

export type HostedLoreServerChoice = (typeof hostedLoreServerChoices)[number];

export const adminSettingsPath = "/api/v1/admin/settings";

export function adminSettingsInput(override: boolean | null): AdminSettingsInput {
  return { hostedLoreServerOverride: override };
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

export function normalizeAdminSettings(value: unknown): AdminSettings | null {
  if (!isRecord(value)) return null;
  if (typeof value.hostedLoreServerEnabled !== "boolean") return null;
  if (typeof value.hostedLoreServerDefault !== "boolean") return null;
  if (value.hostedLoreServerOverride !== null && typeof value.hostedLoreServerOverride !== "boolean") return null;
  return {
    hostedLoreServerEnabled: value.hostedLoreServerEnabled,
    hostedLoreServerOverride: value.hostedLoreServerOverride,
    hostedLoreServerDefault: value.hostedLoreServerDefault,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
