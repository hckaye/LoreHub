import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const conversationPath = "src/components/repositories/pull-request-conversation.tsx";
const detailPath = "src/components/repositories/pull-request-detail.tsx";
const serverAPIPath = "src/lib/lorehub-api.ts";

test("pull request conversation uses persisted comments and CSRF-protected mutations", async () => {
  const [conversation, serverAPI] = await Promise.all([
    readFile(conversationPath, "utf8"),
    readFile(serverAPIPath, "utf8"),
  ]);
  assert.match(serverAPI, /getMergeRequestComments\([\s\S]*commentPageSearch\(page\)/u);
  assert.match(serverAPI, /parseMergeRequestCommentPage\(result\.data, page, conversationCommentPageSize\)/u);
  assert.match(conversation, /postJson<MergeRequestComment>/);
  assert.match(conversation, /patchJson<MergeRequestComment>/);
  assert.match(conversation, /deleteJson<null>/);
  assert.match(conversation, /session\.csrfToken/);
  assert.match(conversation, /viewerCanUpdate/);
});

test("pull request authors and reviewers receive production controls", async () => {
  const [conversation, detail] = await Promise.all([readFile(conversationPath, "utf8"), readFile(detailPath, "utf8")]);
  assert.match(conversation, /If-Match/);
  assert.match(conversation, /viewerCanReview/);
  assert.match(conversation, /postJson<Review>/);
  assert.match(conversation, /changes_requested/);
  assert.doesNotMatch(detail, /returnTo=\{loginUrl/);
});
