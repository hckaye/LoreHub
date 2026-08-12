import { z } from "zod";

import type { BranchRule, MergeReadiness, MergeStatusCheck } from "./api-types";
import { parseBranchRule } from "./branch-rule-contract";
import { parseMergeStatusCheck } from "./commit-status-client";

const timestamp = z
  .string()
  .min(1)
  .refine((value) => Number.isFinite(Date.parse(value)));
const nullableTimestamp = timestamp.nullable();
const nullableString = z.string().nullable();

const mergeRequestSchema = z
  .object({
    id: z.string().min(1),
    number: z.number().int().positive(),
    title: z.string(),
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
  })
  .strict();

const reviewSchema = z
  .object({
    id: z.string().min(1),
    mergeRequestId: z.string().min(1),
    reviewer: z.string().min(1),
    sourceRevision: z.string().min(1),
    decision: z.enum(["approved", "changes_requested", "commented"]),
    body: z.string(),
    createdAt: timestamp,
  })
  .strict();

const reviewSummarySchema = z
  .object({
    currentRevision: z.string().min(1),
    reviews: z.array(reviewSchema),
    currentReviews: z.array(reviewSchema),
    approvals: z.number().int().nonnegative(),
    changeRequests: z.number().int().nonnegative(),
    comments: z.number().int().nonnegative(),
  })
  .strict();

const resolutionSchema = z
  .object({
    path: z.string().min(1),
    strategy: z.enum(["mine", "theirs"]),
    actor: z.string().optional(),
    createdAt: timestamp,
    updatedAt: timestamp,
  })
  .strict();

const mergeOperationSchema = z
  .object({
    id: z.string().min(1),
    mergeRequestId: z.string().min(1),
    repositoryId: z.string().min(1),
    sourceRevision: z.string().min(1),
    targetRevision: z.string().min(1),
    stagedRevision: z.string().optional(),
    pushedRevision: z.string().optional(),
    parentRevisions: z.array(z.string()),
    resolutions: z.array(resolutionSchema),
    state: z.enum(["created", "started", "conflicts", "ready_to_push", "pushing", "pushed", "aborted", "merged"]),
    conflictPaths: z.array(z.string()),
    errorCode: z.string().optional(),
    errorDetail: z.string().optional(),
    version: z.number().int().nonnegative(),
    leaseExpiresAt: timestamp.optional(),
    startedAt: timestamp.optional(),
    completedAt: timestamp.optional(),
    createdAt: timestamp,
    updatedAt: timestamp,
  })
  .strict();

const branchRuleSchema = z.custom<BranchRule>((value) => parseBranchRule(value) !== null);
const statusCheckSchema = z.custom<MergeStatusCheck>((value) => parseMergeStatusCheck(value) !== null);

const mergeReadinessSchema = z
  .object({
    mergeRequest: mergeRequestSchema,
    currentSourceRevision: z.string().min(1),
    currentTargetRevision: z.string().min(1),
    sourceStale: z.boolean(),
    targetStale: z.boolean(),
    canMerge: z.boolean(),
    ready: z.boolean(),
    blockers: z.array(z.object({ code: z.string(), detail: z.string() }).strict()),
    reviews: reviewSummarySchema,
    ciSuccess: z.boolean(),
    statusChecks: z.array(statusCheckSchema),
    directPushBlocked: z.boolean(),
    rules: z.array(branchRuleSchema),
    operation: mergeOperationSchema.optional(),
  })
  .strict();

export function parseMergeReadiness(value: unknown): MergeReadiness | null {
  const result = mergeReadinessSchema.safeParse(value);
  return result.success ? (result.data as MergeReadiness) : null;
}
