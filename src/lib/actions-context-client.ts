export type ActionsContextScopeKind = "organization" | "repository" | "environment";

export type ActionsContextLocation =
  | { kind: "organization"; organization: string }
  | { kind: "repository"; owner: string; repository: string }
  | { kind: "environment"; owner: string; repository: string; environment: string };

export type ActionsContextScope = {
  kind: ActionsContextScopeKind;
  organizationId: string;
  repositoryId: string;
  environment: string;
};

export type ActionsContextEntry = {
  id: string;
  scope: ActionsContextScope;
  name: string;
  secret: boolean;
  value?: string;
  keyId: string;
  updatedBy: string;
  createdAt: string;
  updatedAt: string;
};

export type ActionsContextFailureKind = "unauthorized" | "forbidden" | "not-found" | "invalid" | "unavailable";

export type ActionsContextResult<T> =
  { ok: true; data: T } | { ok: false; kind: ActionsContextFailureKind; code: string | null };

export function actionsContextListPath(location: ActionsContextLocation): string {
  if (location.kind === "organization") {
    return `/api/v1/organizations/${encodeURIComponent(location.organization)}/actions/settings`;
  }
  const base = actionsContextRepositoryBase(location.owner, location.repository);
  const query = new URLSearchParams({ scopeKind: location.kind });
  if (location.kind === "environment") {
    query.set("environment", location.environment);
  }
  return `${base}?${query.toString()}`;
}

export function actionsContextEntryPath(
  location: ActionsContextLocation,
  valueKind: "variable" | "secret",
  name: string,
): string {
  if (location.kind === "organization") {
    const base = `/api/v1/organizations/${encodeURIComponent(location.organization)}/actions/settings`;
    return `${base}/${valueKind}/${encodeURIComponent(name)}`;
  }
  const base = actionsContextRepositoryBase(location.owner, location.repository);
  const path = `${base}/${location.kind}/${valueKind}/${encodeURIComponent(name)}`;
  if (location.kind === "environment") {
    return `${path}?${new URLSearchParams({ environment: location.environment }).toString()}`;
  }
  return path;
}

export async function listActionsContextEntries(
  path: string,
  signal?: AbortSignal,
): Promise<ActionsContextResult<ActionsContextEntry[]>> {
  try {
    const response = await fetch(path, {
      credentials: "include",
      headers: { Accept: "application/json" },
      signal,
    });
    if (!response.ok) {
      return responseFailure(response);
    }
    const payload = (await response.json()) as { entries?: unknown };
    if (!Array.isArray(payload.entries)) {
      return { ok: false, kind: "unavailable", code: "invalid_response" };
    }
    const entries = payload.entries.filter(isActionsContextEntry).map(withoutSecretValue);
    if (entries.length !== payload.entries.length) {
      return { ok: false, kind: "unavailable", code: "invalid_response" };
    }
    return { ok: true, data: entries };
  } catch {
    return { ok: false, kind: "unavailable", code: "network_error" };
  }
}

export async function putActionsContextEntry(
  path: string,
  value: string,
  csrfToken: string,
): Promise<ActionsContextResult<ActionsContextEntry>> {
  try {
    const response = await fetch(path, {
      method: "PUT",
      credentials: "include",
      headers: mutationHeaders(csrfToken, true),
      body: JSON.stringify({ value }),
    });
    if (!response.ok) {
      return responseFailure(response);
    }
    const entry = (await response.json()) as unknown;
    if (!isActionsContextEntry(entry)) {
      return { ok: false, kind: "unavailable", code: "invalid_response" };
    }
    return { ok: true, data: withoutSecretValue(entry) };
  } catch {
    return { ok: false, kind: "unavailable", code: "network_error" };
  }
}

export async function deleteActionsContextEntry(path: string, csrfToken: string): Promise<ActionsContextResult<null>> {
  try {
    const response = await fetch(path, {
      method: "DELETE",
      credentials: "include",
      headers: mutationHeaders(csrfToken, false),
    });
    if (!response.ok) {
      return responseFailure(response);
    }
    return { ok: true, data: null };
  } catch {
    return { ok: false, kind: "unavailable", code: "network_error" };
  }
}

function mutationHeaders(csrfToken: string, hasBody: boolean): HeadersInit {
  const headers: Record<string, string> = {
    Accept: "application/json",
    "X-CSRF-Token": csrfToken,
  };
  if (hasBody) {
    headers["Content-Type"] = "application/json";
  }
  return headers;
}

function actionsContextRepositoryBase(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}` + "/actions/settings";
}

async function responseFailure(response: Response): Promise<ActionsContextResult<never>> {
  return {
    ok: false,
    kind: classifyActionsContextStatus(response.status),
    code: await readProblemCode(response),
  };
}

export function classifyActionsContextStatus(status: number): ActionsContextFailureKind {
  if (status === 401) return "unauthorized";
  if (status === 403) return "forbidden";
  if (status === 404) return "not-found";
  if (status >= 400 && status < 500) return "invalid";
  return "unavailable";
}

function withoutSecretValue(entry: ActionsContextEntry): ActionsContextEntry {
  if (!entry.secret) return entry;
  const metadata = { ...entry };
  delete metadata.value;
  return metadata;
}

function isActionsContextEntry(value: unknown): value is ActionsContextEntry {
  if (!isRecord(value) || !isRecord(value.scope)) return false;
  return (
    typeof value.id === "string" &&
    typeof value.name === "string" &&
    typeof value.secret === "boolean" &&
    (value.value === undefined || typeof value.value === "string") &&
    typeof value.keyId === "string" &&
    typeof value.updatedBy === "string" &&
    typeof value.createdAt === "string" &&
    typeof value.updatedAt === "string" &&
    isActionsContextScope(value.scope)
  );
}

function isActionsContextScope(value: Record<string, unknown>): value is ActionsContextScope {
  return (
    (value.kind === "organization" || value.kind === "repository" || value.kind === "environment") &&
    typeof value.organizationId === "string" &&
    typeof value.repositoryId === "string" &&
    typeof value.environment === "string"
  );
}

async function readProblemCode(response: Response): Promise<string | null> {
  try {
    const payload = (await response.json()) as unknown;
    if (isRecord(payload) && isRecord(payload.error) && typeof payload.error.code === "string") {
      return payload.error.code;
    }
  } catch {
    return null;
  }
  return null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
