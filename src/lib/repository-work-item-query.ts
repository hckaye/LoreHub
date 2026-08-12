export type IssueFilter = "open" | "closed" | "all";
export type MergeRequestFilter = "open" | "closed" | "merged" | "all";
export type RepositoryWorkItemSort = "updated" | "created" | "comments";
export type RepositoryWorkItemDirection = "asc" | "desc";

export type RepositoryIssueQuery = {
  state?: IssueFilter;
  q?: string;
  author?: string;
  assignee?: string;
  labels?: string[];
  milestone?: string;
  sort?: RepositoryWorkItemSort;
  direction?: RepositoryWorkItemDirection;
  page?: number;
  perPage?: number;
};

export type RepositoryMergeRequestQuery = Omit<RepositoryIssueQuery, "state"> & {
  state?: MergeRequestFilter;
  source?: string;
  target?: string;
  draft?: boolean;
};

export type RawRepositoryWorkItemSearchParams = Record<string, string | string[] | undefined>;

export function parseRepositoryIssueQuery(params: RawRepositoryWorkItemSearchParams): RepositoryIssueQuery {
  return {
    state: issueState(single(params.state)),
    ...sharedQuery(params),
  };
}

export function parseRepositoryMergeRequestQuery(
  params: RawRepositoryWorkItemSearchParams,
): RepositoryMergeRequestQuery {
  const draftValue = single(params.draft);
  return {
    state: mergeRequestState(single(params.state)),
    ...sharedQuery(params),
    source: bounded(single(params.source), 255),
    target: bounded(single(params.target), 255),
    draft: draftValue === "true" ? true : draftValue === "false" ? false : undefined,
  };
}

export function repositoryWorkItemSearchParams(
  query: RepositoryIssueQuery | RepositoryMergeRequestQuery,
  changes: Partial<RepositoryMergeRequestQuery> = {},
): URLSearchParams {
  const merged = { ...query, ...changes };
  const params = new URLSearchParams();
  setOptionalParameter(params, "state", nonDefault(merged.state, "open"));
  setOptionalParameter(params, "q", merged.q);
  setOptionalParameter(params, "author", merged.author);
  setOptionalParameter(params, "assignee", merged.assignee);
  setOptionalParameter(params, "milestone", merged.milestone);
  setOptionalParameter(params, "sort", nonDefault(merged.sort, "updated"));
  setOptionalParameter(params, "direction", nonDefault(merged.direction, "desc"));
  setOptionalParameter(params, "page", pageParameter(merged.page));
  setOptionalParameter(params, "per_page", perPageParameter(merged.perPage));
  for (const label of merged.labels ?? []) params.append("label", label);
  setPullRequestParameters(params, merged);
  return params;
}

function setPullRequestParameters(params: URLSearchParams, query: RepositoryIssueQuery | RepositoryMergeRequestQuery) {
  if (!isPullRequestQuery(query)) return;
  setOptionalParameter(params, "source", query.source);
  setOptionalParameter(params, "target", query.target);
  setOptionalParameter(params, "draft", query.draft === undefined ? undefined : String(query.draft));
}

function isPullRequestQuery(
  query: RepositoryIssueQuery | RepositoryMergeRequestQuery,
): query is RepositoryMergeRequestQuery {
  return "source" in query || "target" in query || "draft" in query;
}

function setOptionalParameter(params: URLSearchParams, key: string, value: string | undefined) {
  if (value) params.set(key, value);
}

function nonDefault<T extends string>(value: T | undefined, defaultValue: T): string | undefined {
  return value === undefined || value === defaultValue ? undefined : value;
}

function pageParameter(value: number | undefined): string | undefined {
  return value && value > 1 ? String(value) : undefined;
}

function perPageParameter(value: number | undefined): string | undefined {
  return value && value !== 25 ? String(value) : undefined;
}

function sharedQuery(params: RawRepositoryWorkItemSearchParams): Omit<RepositoryIssueQuery, "state"> {
  return {
    q: bounded(single(params.q), 256),
    author: bounded(single(params.author), 100),
    assignee: bounded(single(params.assignee), 100),
    labels: labels(params.label),
    milestone: milestone(single(params.milestone)),
    sort: sortName(single(params.sort)),
    direction: direction(single(params.direction)),
    page: integer(single(params.page), 1, 10_000, 1),
    perPage: integer(single(params.per_page), 1, 100, 25),
  };
}

function single(value: string | string[] | undefined): string | undefined {
  return typeof value === "string" ? value.trim() : undefined;
}

function bounded(value: string | undefined, maximum: number): string | undefined {
  if (!value || [...value].length > maximum || /\p{Cc}/u.test(value)) return undefined;
  return value;
}

function labels(value: string | string[] | undefined): string[] {
  const values = Array.isArray(value) ? value : value ? [value] : [];
  const unique = new Map<string, string>();
  for (const item of values.flatMap((entry) => entry.split(",")).slice(0, 20)) {
    const label = bounded(item.trim(), 100);
    if (label && !unique.has(label.toLocaleLowerCase("en"))) {
      unique.set(label.toLocaleLowerCase("en"), label);
    }
  }
  return [...unique.values()];
}

function milestone(value: string | undefined): string | undefined {
  if (value === "none") return value;
  if (!/^\d+$/u.test(value ?? "")) return undefined;
  try {
    const number = BigInt(value ?? "0");
    return number > 0n && number <= 9_223_372_036_854_775_807n ? value : undefined;
  } catch {
    return undefined;
  }
}

function integer(value: string | undefined, minimum: number, maximum: number, fallback: number): number {
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}

function issueState(value: string | undefined): IssueFilter {
  return value === "closed" || value === "all" ? value : "open";
}

function mergeRequestState(value: string | undefined): MergeRequestFilter {
  return value === "closed" || value === "merged" || value === "all" ? value : "open";
}

function sortName(value: string | undefined): RepositoryWorkItemSort {
  return value === "created" || value === "comments" ? value : "updated";
}

function direction(value: string | undefined): RepositoryWorkItemDirection {
  return value === "asc" ? value : "desc";
}
