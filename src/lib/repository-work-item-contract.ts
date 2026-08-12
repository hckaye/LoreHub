import { z } from "zod";

import type { MergeRequestListItem, RepositoryIssuePage, RepositoryMergeRequestPage } from "./api-types";

const timestamp = z.string().refine((value) => Number.isFinite(Date.parse(value)));
const nullableTimestamp = timestamp.nullable();
const nullableString = z.string().nullable();

const assigneeSchema = z
  .object({
    id: z.string().min(1),
    username: z.string().min(1),
    displayName: z.string(),
    avatarUrl: z.string(),
  })
  .strict();

const labelSchema = z
  .object({
    id: z.string().min(1),
    repositoryId: z.string().min(1),
    name: z.string().min(1),
    description: z.string(),
    color: z.string().regex(/^[0-9A-Fa-f]{6}$/u),
    createdAt: timestamp,
  })
  .strict();

const milestoneSchema = z
  .object({
    id: z.string().min(1),
    number: z.number().int().positive(),
    title: z.string().min(1),
    state: z.enum(["open", "closed"]),
    dueOn: z
      .string()
      .regex(/^\d{4}-\d{2}-\d{2}$/u)
      .nullable(),
  })
  .strict();

const issueSchema = z
  .object({
    id: z.string().min(1),
    number: z.number().int().positive(),
    title: z.string().min(1),
    body: z.string(),
    state: z.enum(["open", "closed"]),
    author: z.string().min(1),
    assignee: nullableString,
    assignees: z.array(assigneeSchema),
    labels: z.array(labelSchema),
    milestone: milestoneSchema.nullable(),
    labelCount: z.number().int().nonnegative(),
    commentCount: z.number().int().nonnegative(),
    createdAt: timestamp,
    updatedAt: timestamp,
    closedBy: nullableString,
    closedAt: nullableTimestamp,
    viewerCanUpdate: z.boolean(),
    viewerCanManageLabels: z.boolean(),
    viewerCanManageMilestone: z.boolean(),
    viewerCanManageAssignees: z.boolean(),
  })
  .strict()
  .refine((issue) => issue.labelCount === issue.labels.length);

const mergeRequestSchema = z
  .object({
    id: z.string().min(1),
    number: z.number().int().positive(),
    title: z.string().min(1),
    body: z.string(),
    state: z.enum(["open", "closed", "merged"]),
    isDraft: z.boolean(),
    sourceBranch: z.string().min(1),
    targetBranch: z.string().min(1),
    sourceRevision: z.string().min(1),
    targetRevision: z.string().min(1),
    author: z.string().min(1),
    approvalCount: z.number().int().nonnegative(),
    mergedBy: nullableString,
    mergedRevision: nullableString,
    mergedAt: nullableTimestamp,
    createdAt: timestamp,
    updatedAt: timestamp,
    closedAt: nullableTimestamp,
    viewerCanUpdate: z.boolean(),
    viewerCanReview: z.boolean(),
    labels: z.array(labelSchema),
    assignees: z.array(assigneeSchema),
    milestone: milestoneSchema.nullable(),
    commentCount: z.number().int().nonnegative(),
  })
  .strict();

const pageFields = {
  totalCount: z.number().int().nonnegative(),
  openCount: z.number().int().nonnegative(),
  closedCount: z.number().int().nonnegative(),
  page: z.number().int().positive(),
  perPage: z.number().int().min(1).max(100),
  hasNext: z.boolean(),
} as const;

const issuePageSchema = z
  .object({ issues: z.array(issueSchema), ...pageFields })
  .strict()
  .refine((page) => page.issues.length <= page.perPage)
  .refine((page) => page.totalCount <= page.openCount + page.closedCount)
  .refine((page) => page.hasNext === page.totalCount > page.page * page.perPage);

const mergeRequestPageSchema = z
  .object({
    mergeRequests: z.array(mergeRequestSchema),
    ...pageFields,
    mergedCount: z.number().int().nonnegative(),
  })
  .strict()
  .refine((page) => page.mergeRequests.length <= page.perPage)
  .refine((page) => page.totalCount <= page.openCount + page.closedCount + page.mergedCount)
  .refine((page) => page.hasNext === page.totalCount > page.page * page.perPage);

export function parseRepositoryIssuePage(value: unknown): RepositoryIssuePage | null {
  const result = issuePageSchema.safeParse(value);
  return result.success ? (result.data as RepositoryIssuePage) : null;
}

export function parseRepositoryMergeRequestPage(value: unknown): RepositoryMergeRequestPage | null {
  const result = mergeRequestPageSchema.safeParse(value);
  return result.success ? (result.data as RepositoryMergeRequestPage) : null;
}

export function parseMergeRequestListItem(value: unknown): MergeRequestListItem | null {
  const result = mergeRequestSchema.safeParse(value);
  return result.success ? (result.data as MergeRequestListItem) : null;
}
