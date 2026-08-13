export type LoreServer = {
  id: string;
  instanceScope: boolean;
  organizationId: string | null;
  name: string;
  publicUrl: string;
  status: string;
  credentialExpiresAt: string | null;
  lastSeenAt: string | null;
  loreBuildVersion: string;
  revokedAt: string | null;
  createdAt: string;
};

export type LoreServerRegistration = {
  token: string;
  expiresAt: string;
};

export type LoreServerStatus = "active" | "offline" | "revoked";

const heartbeatWindowMs = 300_000;

export function loreServersPath(organization: string): string {
  return `/api/v1/organizations/${encodeURIComponent(organization)}/lore-servers`;
}

export function loreServerRegistrationTokenPath(organization: string): string {
  return `${loreServersPath(organization)}/registration-tokens`;
}

export function loreServerPath(organization: string, loreServerID: string): string {
  return `${loreServersPath(organization)}/${encodeURIComponent(loreServerID)}`;
}

export function defaultLoreServerPath(organization: string): string {
  return `${loreServersPath(organization)}/default`;
}

export function loreServerStatus(server: LoreServer, now = new Date()): LoreServerStatus {
  if (server.revokedAt || server.status === "revoked") return "revoked";
  const lastSeenAt = server.lastSeenAt ? Date.parse(server.lastSeenAt) : Number.NaN;
  if (Number.isNaN(lastSeenAt) || now.getTime() - lastSeenAt > heartbeatWindowMs) return "offline";
  return "active";
}

export function loreServerConfigureCommand(origin: string): string {
  return `lorehub-lores-agent configure --url ${origin}`;
}

export function loreServerRunCommand(): string {
  return "lorehub-lores-agent run";
}

export function normalizeLoreServerList(value: unknown): LoreServer[] | null {
  if (!isRecord(value) || !Array.isArray(value.servers)) return null;
  const servers = value.servers.map(normalizeLoreServer);
  return servers.some((server) => server === null) ? null : (servers as LoreServer[]);
}

export function normalizeDefaultLoreServer(value: unknown): LoreServer | null {
  if (!isRecord(value) || value.server === null || value.server === undefined) return null;
  return normalizeLoreServer(value.server);
}

export function normalizeLoreServerRegistration(value: unknown): LoreServerRegistration | null {
  if (!isRecord(value) || typeof value.value !== "string" || !isRecord(value.token)) return null;
  if (!value.value.startsWith("lhsr_") || typeof value.token.expiresAt !== "string") return null;
  if (Number.isNaN(Date.parse(value.token.expiresAt))) return null;
  return { token: value.value, expiresAt: value.token.expiresAt };
}

export function normalizeLoreServer(value: unknown): LoreServer | null {
  if (!isRecord(value) || typeof value.id !== "string" || typeof value.name !== "string") return null;
  if (typeof value.publicUrl !== "string" || typeof value.status !== "string") return null;
  if (typeof value.createdAt !== "string") return null;
  return {
    id: value.id,
    instanceScope: value.instanceScope === true,
    organizationId: nullableString(value.organizationId),
    name: value.name,
    publicUrl: value.publicUrl,
    status: value.status,
    credentialExpiresAt: nullableString(value.credentialExpiresAt),
    lastSeenAt: nullableString(value.lastSeenAt),
    loreBuildVersion: typeof value.loreBuildVersion === "string" ? value.loreBuildVersion : "",
    revokedAt: nullableString(value.revokedAt),
    createdAt: value.createdAt,
  };
}

function nullableString(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
