export type WebhookEvent =
  | "actions"
  | "branch_rules"
  | "branches"
  | "comments"
  | "issues"
  | "labels"
  | "milestones"
  | "projects"
  | "pull_requests"
  | "releases"
  | "repository"
  | "reviews"
  | "wiki";

export type RepositoryWebhook = {
  id: string;
  url: string;
  events: WebhookEvent[];
  active: boolean;
  secretConfigured: boolean;
  createdAt: string;
  updatedAt: string;
};

export type WebhookDelivery = {
  id: string;
  event: string;
  status: "queued" | "delivering" | "succeeded" | "failed" | "exhausted";
  attemptCount: number;
  responseStatus?: number;
  responseBody: string;
  lastError: string;
  deliveredAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type WebhookInput = {
  url: string;
  events: WebhookEvent[];
  active: boolean;
  secret?: string;
};

export type WebhookFailureKind = "unauthorized" | "forbidden" | "not-found" | "invalid" | "conflict" | "unavailable";

export type WebhookResult<T> = { ok: true; data: T } | { ok: false; kind: WebhookFailureKind; code: string | null };

export function webhookBasePath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/webhooks`;
}

export async function listRepositoryWebhooks(
  owner: string,
  repository: string,
  signal?: AbortSignal,
): Promise<WebhookResult<{ webhooks: RepositoryWebhook[]; availableEvents: WebhookEvent[] }>> {
  try {
    const response = await fetch(webhookBasePath(owner, repository), {
      credentials: "include",
      headers: { Accept: "application/json" },
      signal,
    });
    if (!response.ok) return failure(response);
    const payload = (await response.json()) as { webhooks?: unknown; availableEvents?: unknown };
    if (!Array.isArray(payload.webhooks) || !Array.isArray(payload.availableEvents)) {
      return invalidResponse();
    }
    const webhooks = payload.webhooks.map(parseWebhook);
    const availableEvents = payload.availableEvents.filter(isWebhookEvent);
    if (webhooks.some((item) => item === null) || availableEvents.length !== payload.availableEvents.length) {
      return invalidResponse();
    }
    return {
      ok: true,
      data: { webhooks: webhooks as RepositoryWebhook[], availableEvents },
    };
  } catch {
    return unavailable("network_error");
  }
}

export async function createRepositoryWebhook(
  owner: string,
  repository: string,
  input: WebhookInput & { secret: string },
  csrfToken: string,
): Promise<WebhookResult<RepositoryWebhook>> {
  return mutateWebhook(webhookBasePath(owner, repository), "POST", input, csrfToken);
}

export async function updateRepositoryWebhook(
  owner: string,
  repository: string,
  webhookID: string,
  input: WebhookInput,
  csrfToken: string,
): Promise<WebhookResult<RepositoryWebhook>> {
  const path = `${webhookBasePath(owner, repository)}/${encodeURIComponent(webhookID)}`;
  return mutateWebhook(path, "PATCH", input, csrfToken);
}

export async function deleteRepositoryWebhook(
  owner: string,
  repository: string,
  webhookID: string,
  csrfToken: string,
): Promise<WebhookResult<null>> {
  const path = `${webhookBasePath(owner, repository)}/${encodeURIComponent(webhookID)}`;
  return mutateWithoutBody(path, "DELETE", csrfToken, 204);
}

export async function listWebhookDeliveries(
  owner: string,
  repository: string,
  webhookID: string,
  signal?: AbortSignal,
): Promise<WebhookResult<WebhookDelivery[]>> {
  const path = `${webhookBasePath(owner, repository)}/${encodeURIComponent(webhookID)}/deliveries`;
  try {
    const response = await fetch(path, {
      credentials: "include",
      headers: { Accept: "application/json" },
      signal,
    });
    if (!response.ok) return failure(response);
    const payload = (await response.json()) as { deliveries?: unknown };
    if (!Array.isArray(payload.deliveries)) return invalidResponse();
    const deliveries = payload.deliveries.map(parseDelivery);
    if (deliveries.some((item) => item === null)) return invalidResponse();
    return { ok: true, data: deliveries as WebhookDelivery[] };
  } catch {
    return unavailable("network_error");
  }
}

export async function redeliverWebhook(
  owner: string,
  repository: string,
  webhookID: string,
  deliveryID: string,
  csrfToken: string,
): Promise<WebhookResult<null>> {
  const base = webhookBasePath(owner, repository);
  const path = `${base}/${encodeURIComponent(webhookID)}/deliveries/${encodeURIComponent(deliveryID)}/redeliver`;
  return mutateWithoutBody(path, "POST", csrfToken, 202);
}

async function mutateWebhook(
  path: string,
  method: "POST" | "PATCH",
  input: WebhookInput,
  csrfToken: string,
): Promise<WebhookResult<RepositoryWebhook>> {
  try {
    const response = await fetch(path, {
      method,
      credentials: "include",
      headers: mutationHeaders(csrfToken, true),
      body: JSON.stringify(input),
    });
    if (!response.ok) return failure(response);
    const webhook = parseWebhook(await response.json());
    return webhook === null ? invalidResponse() : { ok: true, data: webhook };
  } catch {
    return unavailable("network_error");
  }
}

async function mutateWithoutBody(
  path: string,
  method: "POST" | "DELETE",
  csrfToken: string,
  expectedStatus: number,
): Promise<WebhookResult<null>> {
  try {
    const response = await fetch(path, {
      method,
      credentials: "include",
      headers: mutationHeaders(csrfToken, false),
    });
    if (!response.ok) return failure(response);
    return response.status === expectedStatus ? { ok: true, data: null } : invalidResponse();
  } catch {
    return unavailable("network_error");
  }
}

function parseWebhook(value: unknown): RepositoryWebhook | null {
  if (
    !isObject(value) ||
    typeof value.id !== "string" ||
    typeof value.url !== "string" ||
    !Array.isArray(value.events) ||
    typeof value.active !== "boolean" ||
    value.secretConfigured !== true ||
    typeof value.createdAt !== "string" ||
    typeof value.updatedAt !== "string"
  ) {
    return null;
  }
  const events = value.events.filter(isWebhookEvent);
  if (events.length !== value.events.length) return null;
  return {
    id: value.id,
    url: value.url,
    events,
    active: value.active,
    secretConfigured: true,
    createdAt: value.createdAt,
    updatedAt: value.updatedAt,
  };
}

function parseDelivery(value: unknown): WebhookDelivery | null {
  if (
    !isObject(value) ||
    typeof value.id !== "string" ||
    typeof value.event !== "string" ||
    !isDeliveryStatus(value.status) ||
    !Number.isInteger(value.attemptCount) ||
    typeof value.responseBody !== "string" ||
    typeof value.lastError !== "string" ||
    typeof value.createdAt !== "string" ||
    typeof value.updatedAt !== "string"
  ) {
    return null;
  }
  if (value.responseStatus !== undefined && !Number.isInteger(value.responseStatus)) return null;
  if (value.deliveredAt !== undefined && typeof value.deliveredAt !== "string") return null;
  return {
    id: value.id,
    event: value.event,
    status: value.status,
    attemptCount: value.attemptCount as number,
    responseStatus: value.responseStatus as number | undefined,
    responseBody: value.responseBody,
    lastError: value.lastError,
    deliveredAt: value.deliveredAt as string | undefined,
    createdAt: value.createdAt,
    updatedAt: value.updatedAt,
  };
}

function isWebhookEvent(value: unknown): value is WebhookEvent {
  return typeof value === "string" && webhookEvents.has(value as WebhookEvent);
}

function isDeliveryStatus(value: unknown): value is WebhookDelivery["status"] {
  return typeof value === "string" && deliveryStatuses.has(value as WebhookDelivery["status"]);
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function mutationHeaders(csrfToken: string, body: boolean): HeadersInit {
  const headers: Record<string, string> = { Accept: "application/json", "X-CSRF-Token": csrfToken };
  if (body) headers["Content-Type"] = "application/json";
  return headers;
}

async function failure(response: Response): Promise<WebhookResult<never>> {
  let code: string | null = null;
  try {
    const payload = (await response.json()) as { error?: { code?: unknown } };
    if (typeof payload.error?.code === "string") code = payload.error.code;
  } catch {
    code = null;
  }
  return { ok: false, kind: classifyWebhookStatus(response.status), code };
}

export function classifyWebhookStatus(status: number): WebhookFailureKind {
  if (status === 401) return "unauthorized";
  if (status === 403) return "forbidden";
  if (status === 404) return "not-found";
  if (status === 400 || status === 413 || status === 415 || status === 422) return "invalid";
  if (status === 409) return "conflict";
  return "unavailable";
}

function invalidResponse<T>(): WebhookResult<T> {
  return unavailable("invalid_response");
}

function unavailable<T>(code: string): WebhookResult<T> {
  return { ok: false, kind: "unavailable", code };
}

const webhookEvents = new Set<WebhookEvent>([
  "actions",
  "branch_rules",
  "branches",
  "comments",
  "issues",
  "labels",
  "milestones",
  "projects",
  "pull_requests",
  "releases",
  "repository",
  "reviews",
  "wiki",
]);

const deliveryStatuses = new Set<WebhookDelivery["status"]>([
  "queued",
  "delivering",
  "succeeded",
  "failed",
  "exhausted",
]);
