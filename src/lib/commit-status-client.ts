import type { APIResult, MergeStatusCheck, RevisionStatus, RevisionStatusPage, RevisionStatusState } from "./api-types";

const states = new Set<RevisionStatusState>(["pending", "success", "failure", "error"]);

export type CreateRevisionStatusInput = {
  state: RevisionStatusState;
  context: string;
  description?: string;
  targetUrl?: string;
  idempotencyKey?: string;
};

export function revisionStatusPath(owner: string, repository: string, revision: string): string {
  const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`;
  return `${base}/revisions/${encodeURIComponent(revision)}/statuses`;
}

export async function createRevisionStatus(
  owner: string,
  repository: string,
  revision: string,
  input: CreateRevisionStatusInput,
  csrfToken: string,
): Promise<APIResult<RevisionStatus>> {
  try {
    const response = await fetch(revisionStatusPath(owner, repository, revision), {
      method: "POST",
      credentials: "include",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
      },
      body: JSON.stringify(input),
    });
    if (!response.ok) return responseFailure(response);
    const status = parseRevisionStatus(await response.json());
    return status ? { ok: true, data: status } : { ok: false, reason: "unavailable" };
  } catch {
    return { ok: false, reason: "unavailable" };
  }
}

export function parseRevisionStatusPage(value: unknown): RevisionStatusPage | null {
  if (!isRecord(value) || !hasExactKeys(value, statusPageKeys)) return null;
  if (
    !isRevision(value.revision) ||
    !isState(value.state) ||
    !Array.isArray(value.statuses) ||
    !Array.isArray(value.history) ||
    !isPositiveInteger(value.page) ||
    !isPositiveInteger(value.perPage) ||
    !isNonNegativeInteger(value.totalCount) ||
    typeof value.hasNext !== "boolean"
  ) {
    return null;
  }
  const statuses = value.statuses.map(parseRevisionStatus);
  const history = value.history.map(parseRevisionStatus);
  if (statuses.some(isNull) || history.some(isNull)) return null;
  if (
    statuses.some((status) => status?.revision !== value.revision) ||
    history.some((status) => status?.revision !== value.revision)
  ) {
    return null;
  }
  return {
    revision: value.revision,
    state: value.state,
    statuses: statuses as RevisionStatus[],
    history: history as RevisionStatus[],
    page: value.page,
    perPage: value.perPage,
    totalCount: value.totalCount,
    hasNext: value.hasNext,
  };
}

export function parseRevisionStatusResponse(result: APIResult<unknown>): APIResult<RevisionStatusPage> {
  if (!result.ok) return result;
  const page = parseRevisionStatusPage(result.data);
  return page ? { ok: true, data: page } : { ok: false, reason: "unavailable" };
}

export function parseRevisionStatus(value: unknown): RevisionStatus | null {
  if (!isRecord(value) || !hasExactKeys(value, statusKeys)) return null;
  if (!hasStatusFields(value) || !isCreator(value.creator)) return null;
  return value as RevisionStatus;
}

export function parseMergeStatusCheck(value: unknown): MergeStatusCheck | null {
  if (!isRecord(value) || !hasExactKeys(value, mergeStatusKeys)) return null;
  if (
    !isBoundedLine(value.context, 1, 100) ||
    !isState(value.state) ||
    !isBoundedLine(value.description, 0, 140) ||
    !isTargetURL(value.targetUrl) ||
    !isBoundedLine(value.creator, 1, 100) ||
    !isTimestamp(value.updatedAt) ||
    typeof value.required !== "boolean"
  ) {
    return null;
  }
  return value as MergeStatusCheck;
}

function hasStatusFields(value: Record<string, unknown>): boolean {
  return (
    typeof value.id === "string" &&
    value.id.length > 0 &&
    isRevision(value.revision) &&
    isBoundedLine(value.context, 1, 100) &&
    isState(value.state) &&
    isBoundedLine(value.description, 0, 140) &&
    isTargetURL(value.targetUrl) &&
    isTimestamp(value.createdAt)
  );
}

function isCreator(value: unknown): boolean {
  return (
    isRecord(value) &&
    hasExactKeys(value, creatorKeys) &&
    typeof value.id === "string" &&
    value.id.length > 0 &&
    typeof value.username === "string" &&
    value.username.length > 0 &&
    typeof value.displayName === "string" &&
    typeof value.avatarUrl === "string"
  );
}

async function responseFailure(response: Response): Promise<APIResult<never>> {
  const code = await readProblemCode(response);
  if (response.status === 401) return { ok: false, reason: "unauthorized", code };
  if (response.status === 403) return { ok: false, reason: "forbidden", code };
  if (response.status === 404) return { ok: false, reason: "not-found", code };
  if (response.status >= 400 && response.status < 500) return { ok: false, reason: "invalid", code };
  return { ok: false, reason: "unavailable", code };
}

async function readProblemCode(response: Response): Promise<string | undefined> {
  try {
    const payload = (await response.json()) as unknown;
    if (isRecord(payload) && isRecord(payload.error) && typeof payload.error.code === "string") {
      return payload.error.code;
    }
  } catch {
    return undefined;
  }
  return undefined;
}

function hasExactKeys(value: Record<string, unknown>, expected: readonly string[]): boolean {
  const keys = Object.keys(value);
  return keys.length === expected.length && expected.every((key) => Object.hasOwn(value, key));
}

function isState(value: unknown): value is RevisionStatusState {
  return typeof value === "string" && states.has(value as RevisionStatusState);
}

function isRevision(value: unknown): value is string {
  return typeof value === "string" && /^[0-9a-f]{64}$/u.test(value);
}

function isTimestamp(value: unknown): value is string {
  const rfc3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/u;
  return typeof value === "string" && rfc3339.test(value) && Number.isFinite(Date.parse(value));
}

function isBoundedLine(value: unknown, minimum: number, maximum: number): value is string {
  return (
    typeof value === "string" && [...value].length >= minimum && [...value].length <= maximum && !/\p{Cc}/u.test(value)
  );
}

function isTargetURL(value: unknown): value is string {
  if (value === "") return true;
  if (typeof value !== "string" || new TextEncoder().encode(value).length > 8_192 || /\p{Cc}/u.test(value)) {
    return false;
  }
  try {
    const parsed = new URL(value);
    return (parsed.protocol === "http:" || parsed.protocol === "https:") && !parsed.username && !parsed.password;
  } catch {
    return false;
  }
}

function isPositiveInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) > 0;
}

function isNonNegativeInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) >= 0;
}

function isNull<T>(value: T | null): value is null {
  return value === null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

const statusPageKeys = [
  "revision",
  "state",
  "statuses",
  "history",
  "page",
  "perPage",
  "totalCount",
  "hasNext",
] as const;

const statusKeys = ["id", "revision", "context", "state", "description", "targetUrl", "creator", "createdAt"] as const;

const creatorKeys = ["id", "username", "displayName", "avatarUrl"] as const;

const mergeStatusKeys = ["context", "state", "description", "targetUrl", "creator", "updatedAt", "required"] as const;
