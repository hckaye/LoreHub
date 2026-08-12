import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { parseIssueCommentPage, parseMergeRequestCommentPage } from "../src/lib/comment-page-contract";
import {
  conversationCommentPageHref,
  lastConversationCommentPage,
  parseConversationCommentPage,
} from "../src/lib/comment-pagination";

const issueComment = {
  id: "comment-1",
  issueId: "issue-1",
  author: "alice",
  body: "First",
  createdAt: "2026-08-12T10:00:00Z",
  editedAt: null,
  viewerCanUpdate: true,
};

const pullRequestComment = {
  id: "comment-2",
  mergeRequestId: "pull-1",
  author: "bob",
  body: "Second",
  createdAt: "2026-08-12T10:01:00Z",
  editedAt: null,
  viewerCanUpdate: false,
};

test("normalizes an issue comment page", () => {
  const items = Array.from({ length: 50 }, (_, index) => ({
    ...issueComment,
    id: `comment-${index + 1}`,
  }));
  const parsed = parseIssueCommentPage({ items, nextCursor: "50", hasMore: true, totalCount: 51 }, 1, 50);
  assert.deepEqual(parsed, {
    items,
    totalCount: 51,
    page: 1,
    perPage: 50,
    hasNext: true,
  });
});

test("normalizes the final pull request comment page", () => {
  const parsed = parseMergeRequestCommentPage({ items: [pullRequestComment], hasMore: false, totalCount: 51 }, 2, 50);
  assert.deepEqual(parsed, {
    items: [pullRequestComment],
    totalCount: 51,
    page: 2,
    perPage: 50,
    hasNext: false,
  });
});

test("rejects incomplete and inconsistent comment pages", () => {
  assert.equal(parseIssueCommentPage({ items: [], hasMore: false }, 1, 50), null);
  assert.equal(
    parseIssueCommentPage({ items: [issueComment], nextCursor: "50", hasMore: false, totalCount: 1 }, 1, 50),
    null,
  );
  assert.equal(
    parseIssueCommentPage({ items: [issueComment], hasMore: false, totalCount: 1, unexpected: true }, 1, 50),
    null,
  );
});

test("validates page query values and links", () => {
  assert.equal(parseConversationCommentPage("2"), 2);
  for (const value of [undefined, "", "0", "100001", "1.5", ["1", "2"]]) {
    assert.equal(parseConversationCommentPage(value), 1);
  }
  assert.equal(conversationCommentPageHref("/en/acme/app/issues/1", 1), "/en/acme/app/issues/1");
  assert.equal(conversationCommentPageHref("/en/acme/app/issues/1", 2), "/en/acme/app/issues/1?comment_page=2");
  assert.equal(lastConversationCommentPage(0, 50), 1);
  assert.equal(lastConversationCommentPage(51, 50), 2);
});

test("issue and pull request detail pages request the selected comment page", async () => {
  const issuePage = await readFile("src/app/[locale]/[owner]/[repository]/issues/[number]/page.tsx", "utf8");
  const pullPage = await readFile("src/app/[locale]/[owner]/[repository]/pulls/[number]/page.tsx", "utf8");
  assert.match(issuePage, /getIssueComments\(owner, repository, number, commentPage\)/u);
  assert.match(pullPage, /getMergeRequestComments\(owner, slug, number, commentPage\)/u);
  assert.match(issuePage, /redirect\(conversationCommentPageHref\(basePath, lastPage\)\)/u);
  assert.match(pullPage, /redirect\(conversationCommentPageHref\(basePath, lastPage\)\)/u);
});
