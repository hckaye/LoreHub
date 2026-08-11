"use client";

import type { ReviewThread, ReviewThreadComment } from "./api-types";
import { deleteJsonWithBody, patchJson, postJson } from "./auth-client";

export type CreateReviewThreadInput = {
  path: string;
  side: "left" | "right";
  lineNumber: number;
  body: string;
  expectedBaseRevision: string;
  expectedHeadRevision: string;
};

export function createReviewThread(
  owner: string,
  repository: string,
  number: number,
  input: CreateReviewThreadInput,
  csrfToken: string,
) {
  return postJson<ReviewThread>(reviewThreadsPath(owner, repository, number), input, csrfToken);
}

export function replyToReviewThread(
  owner: string,
  repository: string,
  number: number,
  threadID: string,
  body: string,
  csrfToken: string,
) {
  return postJson<ReviewThreadComment>(
    `${reviewThreadsPath(owner, repository, number)}/${encodeURIComponent(threadID)}/comments`,
    { body },
    csrfToken,
  );
}

export function updateReviewComment(
  owner: string,
  repository: string,
  number: number,
  threadID: string,
  commentID: string,
  body: string,
  expectedVersion: number,
  csrfToken: string,
) {
  return patchJson<ReviewThreadComment>(
    reviewCommentPath(owner, repository, number, threadID, commentID),
    { body, expectedVersion },
    csrfToken,
  );
}

export function deleteReviewComment(
  owner: string,
  repository: string,
  number: number,
  threadID: string,
  commentID: string,
  expectedVersion: number,
  csrfToken: string,
) {
  return deleteJsonWithBody<null>(
    reviewCommentPath(owner, repository, number, threadID, commentID),
    { expectedVersion },
    csrfToken,
  );
}

export function setReviewThreadResolved(
  owner: string,
  repository: string,
  number: number,
  threadID: string,
  resolved: boolean,
  expectedVersion: number,
  csrfToken: string,
) {
  return patchJson<ReviewThread>(
    `${reviewThreadsPath(owner, repository, number)}/${encodeURIComponent(threadID)}`,
    { resolved, expectedVersion },
    csrfToken,
  );
}

function reviewThreadsPath(owner: string, repository: string, number: number): string {
  return (
    `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}` +
    `/merge-requests/${number}/review-threads`
  );
}

function reviewCommentPath(
  owner: string,
  repository: string,
  number: number,
  threadID: string,
  commentID: string,
): string {
  return (
    `${reviewThreadsPath(owner, repository, number)}/${encodeURIComponent(threadID)}` +
    `/comments/${encodeURIComponent(commentID)}`
  );
}
