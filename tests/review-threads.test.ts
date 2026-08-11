import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import type { LoreDiffFile } from "../src/lib/api-types";
import { parseReviewDiff } from "../src/lib/review-diff";
import {
  createReviewThread,
  deleteReviewComment,
  replyToReviewThread,
  setReviewThreadResolved,
  updateReviewComment,
} from "../src/lib/review-thread-client";

test("review diff parser assigns old and new line numbers", () => {
  const file: LoreDiffFile = {
    path: "src/main.go",
    action: "modified",
    binary: false,
    binaryKnown: true,
    truncated: false,
    patch: "@@ -4,3 +4,4 @@\n same\n-old\n+new\n+extra\n tail\n",
  };
  assert.deepEqual(
    parseReviewDiff(file).map(({ kind, oldLine, newLine, content }) => ({ kind, oldLine, newLine, content })),
    [
      { kind: "header", oldLine: null, newLine: null, content: "@@ -4,3 +4,4 @@" },
      { kind: "context", oldLine: 4, newLine: 4, content: "same" },
      { kind: "deleted", oldLine: 5, newLine: null, content: "old" },
      { kind: "added", oldLine: null, newLine: 5, content: "new" },
      { kind: "added", oldLine: null, newLine: 6, content: "extra" },
      { kind: "context", oldLine: 6, newLine: 7, content: "tail" },
    ],
  );
});

test("review thread client sends CSRF-protected revision and version requests", async () => {
  const requests: Array<{ url: string; init: RequestInit }> = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (input, init = {}) => {
    requests.push({ url: String(input), init });
    const method = init.method ?? "GET";
    if (method === "DELETE") return new Response(null, { status: 204 });
    const isComment = String(input).includes("/comments");
    return Response.json(
      isComment
        ? { id: "comment", author: "alice", body: "body", deleted: false, version: 1 }
        : { id: "thread", path: "a", side: "right", lineNumber: 1, comments: [], version: 1 },
    );
  };
  try {
    await createReviewThread(
      "acme",
      "game",
      7,
      {
        path: "src/main.go",
        side: "right",
        lineNumber: 12,
        body: "Handle this error.",
        expectedBaseRevision: "base",
        expectedHeadRevision: "head",
      },
      "csrf-token",
    );
    await replyToReviewThread("acme", "game", 7, "thread", "Reply", "csrf-token");
    await updateReviewComment("acme", "game", 7, "thread", "comment", "Edit", 2, "csrf-token");
    await deleteReviewComment("acme", "game", 7, "thread", "comment", 3, "csrf-token");
    await setReviewThreadResolved("acme", "game", 7, "thread", true, 4, "csrf-token");
  } finally {
    globalThis.fetch = originalFetch;
  }
  assert.equal(requests.length, 5);
  assert.deepEqual(
    requests.map((request) => request.init.method),
    ["POST", "POST", "PATCH", "DELETE", "PATCH"],
  );
  for (const request of requests) {
    assert.equal((request.init.headers as Record<string, string>)["X-CSRF-Token"], "csrf-token");
    assert.match(request.url, /merge-requests\/7\/review-threads/);
  }
  assert.match(String(requests[0].init.body), /"expectedBaseRevision":"base"/);
  assert.match(String(requests[3].init.body), /"expectedVersion":3/);
});

test("review thread storage validates Lore lines and records audit events", async () => {
  const [migration, handler, store, page] = await Promise.all([
    readFile("services/api/migrations/000035_merge_request_review_threads.sql", "utf8"),
    readFile("services/api/internal/reviewthreads/handlers.go", "utf8"),
    readFile("services/api/internal/reviewthreads/mutation_store.go", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/pulls/[number]/page.tsx", "utf8"),
  ]);
  assert.match(migration, /FOREIGN KEY \(merge_request_id, repository_id\)/);
  assert.match(migration, /FOREIGN KEY \(thread_id, repository_id\)/);
  assert.match(handler, /RevisionDiff/);
  assert.match(handler, /ExpectedBaseRevision/);
  assert.match(store, /insertAudit/);
  assert.match(store, /insertOutbox/);
  assert.match(page, /targetRevision, mergeRequest\.data\.sourceRevision/);
});
