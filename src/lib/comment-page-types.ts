export type CommentPage<T> = {
  items: T[];
  totalCount: number;
  page: number;
  perPage: number;
  hasNext: boolean;
};
