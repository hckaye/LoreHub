export const conversationCommentPageSize = 50;

export function parseConversationCommentPage(value: string | string[] | undefined): number {
  if (typeof value !== "string" || !/^\d+$/u.test(value)) return 1;
  const page = Number(value);
  return Number.isSafeInteger(page) && page >= 1 && page <= 100_000 ? page : 1;
}

export function conversationCommentPageHref(basePath: string, page: number): string {
  return page > 1 ? `${basePath}?comment_page=${page}` : basePath;
}

export function lastConversationCommentPage(totalCount: number, perPage: number): number {
  return Math.max(1, Math.ceil(totalCount / perPage));
}
