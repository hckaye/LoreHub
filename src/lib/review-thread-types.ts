export type ReviewThreadComment = {
  id: string;
  author: string;
  body: string;
  deleted: boolean;
  pending?: boolean;
  version: number;
  createdAt: string;
  updatedAt: string;
  editedAt?: string | null;
  viewerCanUpdate: boolean;
};

export type ReviewThread = {
  id: string;
  path: string;
  side: "left" | "right";
  lineNumber: number;
  lineContent: string;
  baseRevision: string;
  headRevision: string;
  outdated: boolean;
  resolved: boolean;
  version: number;
  createdBy: string;
  resolvedBy?: string | null;
  createdAt: string;
  updatedAt: string;
  resolvedAt?: string | null;
  viewerCanResolve: boolean;
  comments: ReviewThreadComment[];
};

export type PendingReview = {
  id: string;
  author: string;
  body: string;
  commentCount: number;
  createdAt: string;
};

export type ReviewThreadList = {
  threads: ReviewThread[];
  pendingReview: PendingReview | null;
};

export type PendingReviewResult = {
  reviewId: string;
  decision: "approved" | "changes_requested" | "commented";
  body: string;
  publishedComments: number;
  submittedAt: string;
};

export type ReviewVerdict = "approve" | "request_changes" | "comment";
