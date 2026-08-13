import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  discardPendingReview,
  startPendingReview,
  submitPendingReview,
  updatePendingReview,
} from "../src/lib/pending-review-client";
import { createReviewThread, replyToReviewThread } from "../src/lib/review-thread-client";

type RecordedRequest = { url: string; init: RequestInit };

async function recordRequests(run: () => Promise<unknown>): Promise<RecordedRequest[]> {
  const requests: RecordedRequest[] = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (input, init = {}) => {
    requests.push({ url: String(input), init });
    if ((init.method ?? "GET") === "DELETE") return new Response(null, { status: 204 });
    return Response.json({ id: "pending", author: "alice", body: "", commentCount: 1 });
  };
  try {
    await run();
  } finally {
    globalThis.fetch = originalFetch;
  }
  return requests;
}

test("pending review client drives the batched review endpoints with CSRF", async () => {
  const requests = await recordRequests(async () => {
    await startPendingReview("acme", "game", 7, "csrf-token");
    await updatePendingReview("acme", "game", 7, "Almost there.", "csrf-token");
    await submitPendingReview("acme", "game", 7, "request_changes", "Please fix.", "csrf-token");
    await discardPendingReview("acme", "game", 7, "csrf-token");
  });
  assert.deepEqual(
    requests.map((request) => request.init.method),
    ["POST", "PATCH", "POST", "DELETE"],
  );
  for (const request of requests) {
    assert.equal((request.init.headers as Record<string, string>)["X-CSRF-Token"], "csrf-token");
    assert.match(request.url, /merge-requests\/7\/reviews\/pending/);
  }
  assert.match(requests[2].url, /\/reviews\/pending\/submit$/);
  assert.match(String(requests[1].init.body), /"body":"Almost there\."/);
  assert.match(String(requests[2].init.body), /"verdict":"request_changes"/);
  assert.equal(requests[3].init.body, undefined);
});

test("inline comments join a pending review only when one is supplied", async () => {
  const requests = await recordRequests(async () => {
    await createReviewThread(
      "acme",
      "game",
      7,
      {
        path: "src/main.go",
        side: "right",
        lineNumber: 12,
        body: "Batched",
        expectedBaseRevision: "base",
        expectedHeadRevision: "head",
        pendingReviewId: "pending-id",
      },
      "csrf-token",
    );
    await replyToReviewThread("acme", "game", 7, "thread", "Batched reply", "csrf-token", "pending-id");
    await replyToReviewThread("acme", "game", 7, "thread", "Immediate reply", "csrf-token");
  });
  assert.match(String(requests[0].init.body), /"pendingReviewId":"pending-id"/);
  assert.match(String(requests[1].init.body), /"pendingReviewId":"pending-id"/);
  assert.doesNotMatch(String(requests[2].init.body), /pendingReviewId/);
});

test("pending review storage hides comments and publishes them in one transaction", async () => {
  const [migration, reader, store] = await Promise.all([
    readFile("services/api/migrations/000061_pending_reviews.sql", "utf8"),
    readFile("services/api/internal/reviewthreads/read_store.go", "utf8"),
    readFile("services/api/internal/reviewthreads/pending_store.go", "utf8"),
  ]);
  assert.match(migration, /CREATE UNIQUE INDEX pending_reviews_author_unique/);
  assert.match(migration, /FOREIGN KEY \(merge_request_id, repository_id\)/);
  assert.match(migration, /FOREIGN KEY \(pending_review_id, repository_id\)/);
  assert.match(reader, /pending\.id IS NULL OR \(\$3 <> '' AND pending\.author = \$3\)/);
  assert.match(store, /UPDATE merge_request_review_comments SET pending_review_id = NULL/);
  assert.match(store, /INSERT INTO merge_request_reviews/);
  assert.match(store, /DELETE FROM pending_reviews WHERE id = \$1/);
  assert.match(store, /DELETE FROM merge_request_review_comments WHERE pending_review_id = \$1/);
});

test("the files changed tab offers single comments, batched reviews and a review form", async () => {
  const [diffView, bar, card] = await Promise.all([
    readFile("src/components/repositories/review-diff-view.tsx", "utf8"),
    readFile("src/components/repositories/pending-review-bar.tsx", "utf8"),
    readFile("src/components/repositories/review-thread-card.tsx", "utf8"),
  ]);
  assert.match(diffView, /pendingReviews\.addSingleComment/);
  assert.match(diffView, /pendingReviews\.startReview/);
  assert.match(diffView, /startPendingReview/);
  assert.match(bar, /labels\.pendingComments/);
  assert.match(bar, /labels\.finishReview/);
  assert.match(bar, /labels\.abandon\b/);
  assert.match(bar, /submitPendingReview/);
  assert.match(card, /pendingReviews\.pendingBadge/);
});
