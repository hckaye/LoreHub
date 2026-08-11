export type GlobalWorkItemKind = "issue" | "pull_request";
export type GlobalWorkItemState = "all" | "open" | "closed" | "merged";
export type GlobalWorkItemScope = "all" | "involved" | "created" | "assigned" | "review_requested";

export type GlobalWorkItemQuery = {
  state: GlobalWorkItemState;
  scope: GlobalWorkItemScope;
  q?: string;
  cursor?: string;
};

export function globalWorkItemQuery(
  kind: GlobalWorkItemKind,
  input: Record<string, string | string[] | undefined>,
): GlobalWorkItemQuery {
  const stateValue = singleValue(input.state);
  const scopeValue = singleValue(input.scope);
  const q = singleValue(input.q).trim().slice(0, 160);
  const cursorValue = singleValue(input.cursor);
  const state = validQueryState(kind, stateValue) ? stateValue : "open";
  const scope = validQueryScope(kind, scopeValue) ? scopeValue : "involved";
  return {
    state,
    scope,
    ...(q ? { q } : {}),
    ...(cursorValue && cursorValue.length <= 1024 ? { cursor: cursorValue } : {}),
  };
}

export type GlobalWorkItemPage = {
  items: GlobalWorkItem[];
  nextCursor: string | null;
};

export type GlobalWorkItem = {
  id: string;
  kind: GlobalWorkItemKind;
  repository: GlobalWorkItemRepository;
  number: number;
  title: string;
  state: Exclude<GlobalWorkItemState, "all">;
  isDraft: boolean;
  author: GlobalWorkItemUser;
  assignees: GlobalWorkItemUser[];
  labels: GlobalWorkItemLabel[];
  milestone: GlobalWorkItemMilestone | null;
  commentCount: number;
  approvalCount: number;
  sourceBranch: string;
  targetBranch: string;
  createdAt: string;
  updatedAt: string;
};

export type GlobalWorkItemRepository = {
  id: string;
  owner: string;
  slug: string;
  displayName: string;
};

export type GlobalWorkItemUser = {
  id: string;
  username: string;
  displayName: string;
  avatarUrl: string;
};

export type GlobalWorkItemLabel = {
  id: string;
  name: string;
  color: string;
};

export type GlobalWorkItemMilestone = {
  number: number;
  title: string;
};

export function normalizeGlobalWorkItemPage(value: unknown): GlobalWorkItemPage | null {
  if (!isRecord(value) || !Array.isArray(value.items)) return null;
  const items = value.items.map(normalizeGlobalWorkItem);
  if (items.some((item) => item === null)) return null;
  const nextCursor = value.nextCursor ?? null;
  if (nextCursor !== null && typeof nextCursor !== "string") return null;
  return { items: items as GlobalWorkItem[], nextCursor };
}

function normalizeGlobalWorkItem(value: unknown): GlobalWorkItem | null {
  if (!isRecord(value) || !hasWorkItemIdentity(value) || !hasWorkItemDetails(value)) return null;
  const repository = normalizeRepository(value.repository);
  const author = normalizeUser(value.author);
  const assignees = normalizeArray(value.assignees, normalizeUser);
  const labels = normalizeArray(value.labels, normalizeLabel);
  const milestone = value.milestone === null ? null : normalizeMilestone(value.milestone);
  if (!repository || !author || !assignees || !labels || (value.milestone !== null && !milestone)) return null;
  return {
    id: value.id,
    kind: value.kind,
    repository,
    number: value.number,
    title: value.title,
    state: value.state,
    isDraft: value.isDraft,
    author,
    assignees,
    labels,
    milestone,
    commentCount: value.commentCount,
    approvalCount: value.approvalCount,
    sourceBranch: typeof value.sourceBranch === "string" ? value.sourceBranch : "",
    targetBranch: typeof value.targetBranch === "string" ? value.targetBranch : "",
    createdAt: value.createdAt,
    updatedAt: value.updatedAt,
  };
}

type WorkItemIdentityFields = {
  id: string;
  kind: GlobalWorkItemKind;
  number: number;
  title: string;
  state: Exclude<GlobalWorkItemState, "all">;
  isDraft: boolean;
};

type WorkItemDetailFields = {
  commentCount: number;
  approvalCount: number;
  sourceBranch?: string;
  targetBranch?: string;
  createdAt: string;
  updatedAt: string;
};

function hasWorkItemIdentity(
  value: Record<string, unknown>,
): value is Record<string, unknown> & WorkItemIdentityFields {
  return (
    isString(value.id) &&
    isKind(value.kind) &&
    isInteger(value.number) &&
    isString(value.title) &&
    isState(value.state) &&
    typeof value.isDraft === "boolean"
  );
}

function hasWorkItemDetails(value: Record<string, unknown>): value is Record<string, unknown> & WorkItemDetailFields {
  return (
    isCount(value.commentCount) &&
    isCount(value.approvalCount) &&
    isString(value.sourceBranch ?? "") &&
    isString(value.targetBranch ?? "") &&
    isDate(value.createdAt) &&
    isDate(value.updatedAt)
  );
}

function normalizeRepository(value: unknown): GlobalWorkItemRepository | null {
  if (!isRecord(value) || !isString(value.id) || !isString(value.owner) || !isString(value.slug)) return null;
  if (!isString(value.displayName)) return null;
  return { id: value.id, owner: value.owner, slug: value.slug, displayName: value.displayName };
}

function normalizeUser(value: unknown): GlobalWorkItemUser | null {
  if (!isRecord(value) || !isString(value.id) || !isString(value.username)) return null;
  if (!isString(value.displayName) || !isString(value.avatarUrl)) return null;
  return { id: value.id, username: value.username, displayName: value.displayName, avatarUrl: value.avatarUrl };
}

function normalizeLabel(value: unknown): GlobalWorkItemLabel | null {
  if (!isRecord(value) || !isString(value.id) || !isString(value.name) || !isString(value.color)) return null;
  if (!/^[0-9A-Fa-f]{6}$/.test(value.color)) return null;
  return { id: value.id, name: value.name, color: value.color };
}

function normalizeMilestone(value: unknown): GlobalWorkItemMilestone | null {
  if (!isRecord(value) || !isInteger(value.number) || !isString(value.title)) return null;
  return { number: value.number, title: value.title };
}

function normalizeArray<T>(value: unknown, normalize: (item: unknown) => T | null): T[] | null {
  if (!Array.isArray(value)) return null;
  const result = value.map(normalize);
  return result.some((item) => item === null) ? null : (result as T[]);
}

function isKind(value: unknown): value is GlobalWorkItemKind {
  return value === "issue" || value === "pull_request";
}

function validQueryState(kind: GlobalWorkItemKind, value: string): value is GlobalWorkItemState {
  if (value === "all" || value === "open" || value === "closed") return true;
  return kind === "pull_request" && value === "merged";
}

function validQueryScope(kind: GlobalWorkItemKind, value: string): value is GlobalWorkItemScope {
  if (value === "all" || value === "involved" || value === "created" || value === "assigned") return true;
  return kind === "pull_request" && value === "review_requested";
}

function singleValue(value: string | string[] | undefined): string {
  return typeof value === "string" ? value : "";
}

function isState(value: unknown): value is Exclude<GlobalWorkItemState, "all"> {
  return value === "open" || value === "closed" || value === "merged";
}

function isString(value: unknown): value is string {
  return typeof value === "string";
}

function isInteger(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) > 0;
}

function isCount(value: unknown): value is number {
  return Number.isSafeInteger(value) && Number(value) >= 0;
}

function isDate(value: unknown): value is string {
  return typeof value === "string" && !Number.isNaN(Date.parse(value));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
