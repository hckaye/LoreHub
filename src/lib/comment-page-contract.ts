import { z } from "zod";

import type { IssueComment, MergeRequestComment } from "./api-types";
import type { CommentPage } from "./comment-page-types";
import { optionalReactionsSchema } from "./reaction-contract";

const timestamp = z.string().refine((value) => Number.isFinite(Date.parse(value)));

const issueCommentSchema = z
  .object({
    id: z.string().min(1),
    issueId: z.string().min(1),
    author: z.string().min(1),
    body: z.string(),
    createdAt: timestamp,
    editedAt: timestamp.nullable(),
    reactions: optionalReactionsSchema,
    viewerCanUpdate: z.boolean(),
  })
  .strict();

const mergeRequestCommentSchema = z
  .object({
    id: z.string().min(1),
    mergeRequestId: z.string().min(1),
    author: z.string().min(1),
    body: z.string(),
    createdAt: timestamp,
    editedAt: timestamp.nullable(),
    reactions: optionalReactionsSchema,
    viewerCanUpdate: z.boolean(),
  })
  .strict();

function rawCommentPageSchema<T extends z.ZodType>(item: T) {
  return z
    .object({
      items: z.array(item),
      nextCursor: z.string().regex(/^\d+$/u).optional(),
      hasMore: z.boolean(),
      totalCount: z.number().int().nonnegative(),
    })
    .strict();
}

const issueCommentPageSchema = rawCommentPageSchema(issueCommentSchema);
const mergeRequestCommentPageSchema = rawCommentPageSchema(mergeRequestCommentSchema);

export function parseIssueCommentPage(value: unknown, page: number, perPage: number): CommentPage<IssueComment> | null {
  const result = issueCommentPageSchema.safeParse(value);
  return result.success ? normalizeCommentPage(result.data, page, perPage) : null;
}

export function parseMergeRequestCommentPage(
  value: unknown,
  page: number,
  perPage: number,
): CommentPage<MergeRequestComment> | null {
  const result = mergeRequestCommentPageSchema.safeParse(value);
  return result.success ? normalizeCommentPage(result.data, page, perPage) : null;
}

function normalizeCommentPage<T>(
  value: { items: T[]; nextCursor?: string; hasMore: boolean; totalCount: number },
  page: number,
  perPage: number,
): CommentPage<T> | null {
  if (!Number.isSafeInteger(page) || page < 1 || !Number.isSafeInteger(perPage) || perPage < 1) return null;
  const offset = (page - 1) * perPage;
  const expectedCount = Math.max(0, Math.min(perPage, value.totalCount - offset));
  const hasNext = value.totalCount > page * perPage;
  const expectedCursor = hasNext ? String(page * perPage) : undefined;
  if (value.items.length !== expectedCount || value.hasMore !== hasNext || value.nextCursor !== expectedCursor) {
    return null;
  }
  return { items: value.items, totalCount: value.totalCount, page, perPage, hasNext };
}
