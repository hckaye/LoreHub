"use client";

import type { PendingReview, PendingReviewResult, ReviewVerdict } from "./api-types";
import { deleteJson, patchJson, postJson } from "./auth-client";

export function startPendingReview(owner: string, repository: string, number: number, csrfToken: string) {
  return postJson<PendingReview>(pendingReviewPath(owner, repository, number), {}, csrfToken);
}

export function updatePendingReview(
  owner: string,
  repository: string,
  number: number,
  body: string,
  csrfToken: string,
) {
  return patchJson<PendingReview>(pendingReviewPath(owner, repository, number), { body }, csrfToken);
}

export function submitPendingReview(
  owner: string,
  repository: string,
  number: number,
  verdict: ReviewVerdict,
  body: string,
  csrfToken: string,
) {
  return postJson<PendingReviewResult>(
    `${pendingReviewPath(owner, repository, number)}/submit`,
    { verdict, body },
    csrfToken,
  );
}

export function discardPendingReview(owner: string, repository: string, number: number, csrfToken: string) {
  return deleteJson<null>(pendingReviewPath(owner, repository, number), csrfToken);
}

function pendingReviewPath(owner: string, repository: string, number: number): string {
  return (
    `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}` +
    `/merge-requests/${number}/reviews/pending`
  );
}
