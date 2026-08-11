import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const conversationPath = "src/components/repositories/pull-request-conversation.tsx";
const detailPath = "src/components/repositories/pull-request-detail.tsx";
const formPath = "src/components/repositories/pull-request-form.tsx";
const listPath = "src/components/repositories/merge-request-list.tsx";

test("draft pull requests can be created and changed with CSRF-protected requests", async () => {
  const [conversation, form] = await Promise.all([readFile(conversationPath, "utf8"), readFile(formPath, "utf8")]);
  assert.match(form, /isDraft/);
  assert.match(form, /createDraft/);
  assert.match(conversation, /putJson<MergeRequest>/);
  assert.match(conversation, /deleteJson<MergeRequest>/);
  assert.match(conversation, /session\.csrfToken/);
});

test("draft state appears in pull request lists and details", async () => {
  const [detail, list] = await Promise.all([readFile(detailPath, "utf8"), readFile(listPath, "utf8")]);
  assert.match(detail, /mergeRequest\.isDraft/);
  assert.match(detail, /pullRequestDrafts\.badge/);
  assert.match(list, /mergeRequest\.isDraft/);
  assert.match(list, /pullRequestDrafts\.badge/);
});
