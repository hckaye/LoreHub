import { z } from "zod";

const timestamp = z.string().refine((value) => Number.isFinite(Date.parse(value)));
const revision = z.string().regex(/^[0-9a-f]{64}$/u);

const authorSchema = z
  .object({
    id: z.string().uuid(),
    username: z.string().min(1),
    displayName: z.string(),
    avatarUrl: z.string(),
  })
  .strict();

export const revisionCommentSchema = z
  .object({
    id: z.string().uuid(),
    revision,
    author: authorSchema,
    body: z.string().min(1).max(1_000_000),
    createdAt: timestamp,
    editedAt: timestamp.nullable(),
    viewerCanUpdate: z.boolean(),
  })
  .strict();

const pageSchema = z
  .object({
    items: z.array(revisionCommentSchema),
    page: z.number().int().min(1).max(1_000_000),
    perPage: z.number().int().min(1).max(100),
    totalCount: z.number().int().nonnegative(),
    hasNext: z.boolean(),
  })
  .strict();

export type RevisionComment = z.infer<typeof revisionCommentSchema>;
export type RevisionCommentPage = z.infer<typeof pageSchema>;

export const revisionCommentPageSize = 30;

export function parseRevisionCommentPage(value: unknown, expectedRevision: string): RevisionCommentPage | null {
  const result = pageSchema.safeParse(value);
  if (!result.success) return null;
  const page = result.data;
  if (page.items.some((comment) => comment.revision !== expectedRevision)) return null;
  const offset = (page.page - 1) * page.perPage;
  const expectedCount = Math.max(0, Math.min(page.perPage, page.totalCount - offset));
  const expectedHasNext = page.totalCount > page.page * page.perPage;
  if (page.hasNext !== expectedHasNext || page.items.length !== expectedCount) return null;
  return page;
}

export function parseRevisionComment(value: unknown): RevisionComment | null {
  const result = revisionCommentSchema.safeParse(value);
  return result.success ? result.data : null;
}

export function revisionCommentsAPIPath(owner: string, repository: string, value: string): string {
  const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`;
  return `${base}/revisions/${encodeURIComponent(value)}/comments`;
}

export function parseRevisionCommentPageNumber(value: string | string[] | undefined): number {
  if (typeof value !== "string" || !/^\d+$/u.test(value)) return 1;
  const page = Number(value);
  return Number.isSafeInteger(page) && page >= 1 && page <= 1_000_000 ? page : 1;
}

export function lastRevisionCommentPage(totalCount: number, perPage: number): number {
  return Math.max(1, Math.ceil(totalCount / perPage));
}

export function revisionCommentPageHref(basePath: string, value: string, page: number): string {
  const query = new URLSearchParams({ revision: value });
  if (page > 1) query.set("commentPage", String(page));
  return `${basePath}?${query.toString()}`;
}
