import { z } from "zod";

import type { OrganizationView, Repository } from "./api-types";
import type { GlobalWorkItem } from "./global-work-items";

export const searchPageSize = 20;

export const searchTypes = ["all", "repositories", "organizations", "users", "issues", "pulls"] as const;

export type SearchType = (typeof searchTypes)[number];

export type SearchQuery = {
  q: string;
  type: SearchType;
  page: number;
};

export type SearchUser = {
  id: string;
  username: string;
  displayName: string;
  avatarUrl: string;
};

export type SearchRepository = Repository & {
  lifecycleState?: string;
  provisioningError?: string;
};

export type SearchCounts = {
  repositories: number;
  organizations: number;
  users: number;
  issues: number;
  pullRequests: number;
};

export type SearchResults = {
  repositories: SearchRepository[];
  organizations: OrganizationView[];
  users: SearchUser[];
  issues: GlobalWorkItem[];
  pullRequests: GlobalWorkItem[];
  counts: SearchCounts;
  page: number;
  perPage: number;
};

const identifier = z.string().min(1);
const count = z.number().int().nonnegative();
const dateTime = z.string().datetime({ offset: true });
const visibility = z.enum(["private", "internal", "public"]);

const repositorySchema = z
  .object({
    id: identifier,
    organizationId: identifier,
    owner: identifier,
    slug: identifier,
    displayName: z.string(),
    description: z.string(),
    visibility,
    loreRepositoryId: identifier,
    loreUrl: z.string(),
    defaultBranch: z.string(),
    homepageUrl: z.string(),
    allowIssues: z.boolean(),
    allowMergeRequests: z.boolean(),
    topics: z.array(z.string()),
    issueCount: count,
    mergeRequestCount: count,
    archivedAt: dateTime.nullable(),
    updatedAt: dateTime,
    lifecycleState: z.string().optional(),
    provisioningError: z.string().optional(),
  })
  .strict();

const organizationSchema = z
  .object({
    id: identifier,
    slug: identifier,
    displayName: z.string(),
    description: z.string(),
    visibility,
    createdAt: dateTime,
    websiteUrl: z.string(),
    contactEmail: z.string(),
    defaultRepositoryVisibility: visibility,
    role: z.enum(["", "owner", "maintainer", "member"]),
    memberCount: count,
    repositoryCount: count,
    teamCount: count,
  })
  .strict();

const userSchema = z
  .object({
    id: identifier,
    username: identifier,
    displayName: z.string(),
    avatarUrl: z.string(),
  })
  .strict();

const workItemUserSchema = userSchema;
const workItemSchema = z
  .object({
    id: identifier,
    kind: z.enum(["issue", "pull_request"]),
    repository: z
      .object({
        id: identifier,
        owner: identifier,
        slug: identifier,
        displayName: z.string(),
      })
      .strict(),
    number: z.number().int().positive(),
    title: z.string(),
    state: z.enum(["open", "closed", "merged"]),
    isDraft: z.boolean(),
    author: workItemUserSchema,
    assignees: z.array(workItemUserSchema),
    labels: z.array(
      z.object({ id: identifier, name: z.string(), color: z.string().regex(/^[0-9A-Fa-f]{6}$/) }).strict(),
    ),
    milestone: z.object({ number: z.number().int().positive(), title: z.string() }).strict().nullable(),
    commentCount: count,
    approvalCount: count,
    sourceBranch: z.string().optional().default(""),
    targetBranch: z.string().optional().default(""),
    createdAt: dateTime,
    updatedAt: dateTime,
  })
  .strict();

const searchResultsSchema = z
  .object({
    repositories: z.array(repositorySchema),
    organizations: z.array(organizationSchema),
    users: z.array(userSchema),
    issues: z.array(workItemSchema),
    pullRequests: z.array(workItemSchema),
    counts: z
      .object({
        repositories: count,
        organizations: count,
        users: count,
        issues: count,
        pullRequests: count,
      })
      .strict(),
    page: z.number().int().min(1).max(100_000),
    perPage: z.literal(searchPageSize),
  })
  .strict();

export function normalizeSearchQuery(input: Record<string, string | string[] | undefined>): SearchQuery {
  const type = singleValue(input.type);
  return {
    q: singleValue(input.q).trim().slice(0, 160),
    type: isSearchType(type) ? type : "all",
    page: normalizePage(singleValue(input.page)),
  };
}

export function parseSearchResults(value: unknown, query: SearchQuery): SearchResults | null {
  const parsed = searchResultsSchema.safeParse(value);
  if (!parsed.success || parsed.data.page !== query.page) return null;
  if (!validWorkItems(parsed.data.issues, "issue") || !validWorkItems(parsed.data.pullRequests, "pull_request")) {
    return null;
  }
  if (!validPageLengths(parsed.data, query.type)) return null;
  return parsed.data;
}

export function searchHref(locale: string, query: SearchQuery, overrides: Partial<SearchQuery> = {}): string {
  const values = { ...query, ...overrides };
  const params = new URLSearchParams({ q: values.q, type: values.type });
  if (values.page > 1) params.set("page", String(values.page));
  return `/${locale}/search?${params.toString()}`;
}

export function lastSearchPage(results: SearchResults, type: SearchType): number {
  const total = type === "all" ? Math.max(...Object.values(results.counts)) : searchTypeCount(results.counts, type);
  return Math.max(1, Math.ceil(total / searchPageSize));
}

export function searchTotalCount(counts: SearchCounts): number {
  return Object.values(counts).reduce((total, value) => total + value, 0);
}

export function searchTypeCount(counts: SearchCounts, type: SearchType): number {
  return type === "all" ? searchTotalCount(counts) : counts[type === "pulls" ? "pullRequests" : type];
}

function validPageLengths(results: SearchResults, type: SearchType): boolean {
  const entries = [
    ["repositories", results.repositories],
    ["organizations", results.organizations],
    ["users", results.users],
    ["issues", results.issues],
    ["pullRequests", results.pullRequests],
  ] as const;
  return entries.every(([key, items]) => {
    const selected = type === "all" || key === type || (type === "pulls" && key === "pullRequests");
    if (!selected) return items.length === 0;
    const remaining = Math.max(0, results.counts[key] - (results.page - 1) * searchPageSize);
    return items.length === Math.min(searchPageSize, remaining);
  });
}

function validWorkItems(items: GlobalWorkItem[], kind: GlobalWorkItem["kind"]): boolean {
  return items.every((item) => {
    if (item.kind !== kind) return false;
    return kind === "issue"
      ? !item.sourceBranch && !item.targetBranch
      : Boolean(item.sourceBranch && item.targetBranch);
  });
}

function isSearchType(value: string): value is SearchType {
  return searchTypes.some((type) => type === value);
}

function normalizePage(value: string): number {
  if (!/^\d+$/.test(value)) return 1;
  const page = Number(value);
  return Number.isSafeInteger(page) && page >= 1 && page <= 100_000 ? page : 1;
}

function singleValue(value: string | string[] | undefined): string {
  return typeof value === "string" ? value : "";
}
