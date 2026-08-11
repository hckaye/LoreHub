import type {
  CreatedPersonalAccessToken,
  PersonalAccessToken,
  PersonalAccessTokenPage,
  PersonalAccessTokenScope,
} from "./api-types";

const scopes = new Set<PersonalAccessTokenScope>(["read_api", "api", "read_repository", "write_repository"]);

export function normalizePersonalAccessTokenPage(value: unknown): PersonalAccessTokenPage | null {
  if (!isRecord(value) || !Array.isArray(value.tokens)) return null;
  const tokens = value.tokens.map(normalizePersonalAccessToken);
  return tokens.every((token): token is PersonalAccessToken => token !== null) ? { tokens } : null;
}

export function normalizeCreatedPersonalAccessToken(value: unknown): CreatedPersonalAccessToken | null {
  if (!isRecord(value) || !validTokenValue(value.value)) return null;
  const token = normalizePersonalAccessToken(value.token);
  return token ? { token, value: value.value } : null;
}

function normalizePersonalAccessToken(value: unknown): PersonalAccessToken | null {
  if (!isRecord(value) || !Array.isArray(value.scopes)) return null;
  const id = readString(value.id);
  const name = readString(value.name);
  const prefix = readString(value.prefix);
  const expiresAt = readTimestamp(value.expiresAt);
  const createdAt = readTimestamp(value.createdAt);
  const lastUsedAt = readNullableTimestamp(value.lastUsedAt);
  const revokedAt = readNullableTimestamp(value.revokedAt);
  const tokenScopes = value.scopes.filter(isPersonalAccessTokenScope);
  if (
    !id ||
    !name ||
    !prefix?.match(/^lhp_[A-Za-z0-9_-]{8}$/) ||
    !expiresAt ||
    !createdAt ||
    lastUsedAt === undefined ||
    revokedAt === undefined ||
    tokenScopes.length === 0 ||
    tokenScopes.length !== value.scopes.length ||
    new Set(tokenScopes).size !== tokenScopes.length
  ) {
    return null;
  }
  return { id, name, prefix, scopes: tokenScopes, expiresAt, createdAt, lastUsedAt, revokedAt };
}

function validTokenValue(value: unknown): value is string {
  return typeof value === "string" && /^lhp_[A-Za-z0-9_-]{43}$/.test(value);
}

function isPersonalAccessTokenScope(value: unknown): value is PersonalAccessTokenScope {
  return typeof value === "string" && scopes.has(value as PersonalAccessTokenScope);
}

function readNullableTimestamp(value: unknown): string | null | undefined {
  if (value === null) return null;
  return readTimestamp(value) ?? undefined;
}

function readTimestamp(value: unknown): string | null {
  return typeof value === "string" && Number.isFinite(Date.parse(value)) ? value : null;
}

function readString(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value : null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
