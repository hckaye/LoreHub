export type RunnerScope = {
  organizationId?: string;
  repositoryId?: string;
  userId?: string;
};

export type Runner = {
  id: string;
  scope: RunnerScope;
  name: string;
  labels: string[];
  credentialExpiresAt: string;
  lastUsedAt: string | null;
  revokedAt: string | null;
  runnerVersion: string;
  lastSeenAt: string | null;
  createdAt: string;
};

export type RunnerTarget =
  { kind: "repository"; owner: string; repository: string } | { kind: "organization"; organization: string };

export type RunnerStatus = "idle" | "offline" | "expired" | "revoked";

export type RunnerRegistration = {
  token: string;
  expiresAt: string;
};

const heartbeatWindowMs = 120_000;

export function runnersPath(target: RunnerTarget): string {
  if (target.kind === "repository") {
    const owner = encodeURIComponent(target.owner);
    const repository = encodeURIComponent(target.repository);
    return `/api/v1/repositories/${owner}/${repository}/actions/runners`;
  }
  return `/api/v1/organizations/${encodeURIComponent(target.organization)}/actions/runners`;
}

export function runnerRegistrationTokenPath(target: RunnerTarget): string {
  return `${runnersPath(target)}/registration-token`;
}

export function runnerPath(target: RunnerTarget, runnerID: string): string {
  return `${runnersPath(target)}/${encodeURIComponent(runnerID)}`;
}

export function runnerStatus(runner: Runner, now = new Date()): RunnerStatus {
  if (runner.revokedAt) return "revoked";
  const expiresAt = Date.parse(runner.credentialExpiresAt);
  if (!Number.isNaN(expiresAt) && expiresAt <= now.getTime()) return "expired";
  const lastSeenAt = runner.lastSeenAt ? Date.parse(runner.lastSeenAt) : Number.NaN;
  if (Number.isNaN(lastSeenAt) || now.getTime() - lastSeenAt > heartbeatWindowMs) return "offline";
  return "idle";
}

export function runnerConfigureCommand(origin: string): string {
  return `lorehub-runner configure --url ${origin}`;
}

export function runnerRunCommand(): string {
  return "lorehub-runner run";
}

export function normalizeRunnerList(value: unknown): Runner[] | null {
  if (!isRecord(value) || !Array.isArray(value.runners)) return null;
  const runners = value.runners.map(normalizeRunner);
  return runners.some((runner) => runner === null) ? null : (runners as Runner[]);
}

export function normalizeRunnerRegistration(value: unknown): RunnerRegistration | null {
  if (!isRecord(value) || typeof value.token !== "string" || typeof value.expiresAt !== "string") {
    return null;
  }
  if (!value.token.startsWith("lhrr_") || Number.isNaN(Date.parse(value.expiresAt))) return null;
  return { token: value.token, expiresAt: value.expiresAt };
}

function normalizeRunner(value: unknown): Runner | null {
  if (!isRecord(value) || !isRecord(value.scope) || !Array.isArray(value.labels)) return null;
  if (typeof value.id !== "string" || typeof value.name !== "string") return null;
  if (typeof value.credentialExpiresAt !== "string" || typeof value.createdAt !== "string") return null;
  if (value.labels.some((label) => typeof label !== "string")) return null;
  return {
    id: value.id,
    scope: {
      organizationId: optionalString(value.scope.organizationId),
      repositoryId: optionalString(value.scope.repositoryId),
      userId: optionalString(value.scope.userId),
    },
    name: value.name,
    labels: value.labels as string[],
    credentialExpiresAt: value.credentialExpiresAt,
    lastUsedAt: nullableString(value.lastUsedAt),
    revokedAt: nullableString(value.revokedAt),
    runnerVersion: typeof value.runnerVersion === "string" ? value.runnerVersion : "",
    lastSeenAt: nullableString(value.lastSeenAt),
    createdAt: value.createdAt,
  };
}

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function nullableString(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
