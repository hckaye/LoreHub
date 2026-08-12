import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { revisionComments } from "../src/i18n/dictionaries/revision-comments";
import {
  createRevisionComment,
  deleteRevisionComment,
  updateRevisionComment,
} from "../src/lib/revision-comment-client";
import {
  parseRevisionCommentPage,
  parseRevisionCommentPageNumber,
  revisionCommentPageHref,
  revisionCommentsAPIPath,
} from "../src/lib/revision-comments";

const revision = "a".repeat(64);
const commentID = "44444444-4444-4444-8444-444444444444";

test("revision comment parser accepts the exact paginated contract", () => {
  const parsed = parseRevisionCommentPage(commentPage(), revision);
  assert.ok(parsed);
  assert.equal(parsed.items[0]?.author.username, "alice");
  assert.equal(parsed.totalCount, 1);

  const extra = commentPage() as ReturnType<typeof commentPage> & { legacy?: boolean };
  extra.legacy = true;
  assert.equal(parseRevisionCommentPage(extra, revision), null);

  const wrongRevision = commentPage();
  wrongRevision.items[0]!.revision = "b".repeat(64);
  assert.equal(parseRevisionCommentPage(wrongRevision, revision), null);

  const missingItem = { ...commentPage(), items: [] };
  assert.equal(parseRevisionCommentPage(missingItem, revision), null);
});

test("revision comment paging preserves the Lore revision", () => {
  assert.equal(parseRevisionCommentPageNumber("2"), 2);
  for (const value of [undefined, "", "0", "1000001", "1.5", ["1", "2"]]) {
    assert.equal(parseRevisionCommentPageNumber(value), 1);
  }
  assert.equal(
    revisionCommentPageHref("/en/acme/app/commit", revision, 2),
    `/en/acme/app/commit?revision=${revision}&commentPage=2`,
  );
});

test("revision comment mutations include CSRF and validate responses", async () => {
  const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
  const previousFetch = globalThis.fetch;
  globalThis.fetch = async (input, init) => {
    requests.push({ input, init });
    if (init?.method === "DELETE") return new Response(null, { status: 204 });
    return Response.json(commentRecord(), { status: init?.method === "POST" ? 201 : 200 });
  };
  try {
    const created = await createRevisionComment("acme team", "game/client", revision, "First", "csrf-token");
    const updated = await updateRevisionComment(
      "acme team",
      "game/client",
      revision,
      commentID,
      "Updated",
      "csrf-token",
    );
    const deleted = await deleteRevisionComment("acme team", "game/client", revision, commentID, "csrf-token");
    assert.equal(created.ok, true);
    assert.equal(updated.ok, true);
    assert.equal(deleted.ok, true);
    assert.equal(requests[0]?.input, revisionCommentsAPIPath("acme team", "game/client", revision));
    assert.equal(new Headers(requests[0]?.init?.headers).get("X-CSRF-Token"), "csrf-token");
    assert.deepEqual(JSON.parse(String(requests[0]?.init?.body)), { body: "First" });
    assert.match(String(requests[1]?.input), new RegExp(`${commentID}$`, "u"));
    assert.equal(requests[2]?.init?.method, "DELETE");
  } finally {
    globalThis.fetch = previousFetch;
  }
});

test("commit page renders revision comments and English and Japanese copy stay paired", async () => {
  const page = await readFile("src/app/[locale]/[owner]/[repository]/commit/page.tsx", "utf8");
  assert.match(page, /getRevisionComments\(owner, slug, revision\.data\.revision, commentPage\)/u);
  assert.match(page, /<RevisionComments/u);
  assert.deepEqual(Object.keys(revisionComments.en), Object.keys(revisionComments.ja));
});

function commentPage() {
  return {
    items: [commentRecord()],
    page: 1,
    perPage: 30,
    totalCount: 1,
    hasNext: false,
  };
}

function commentRecord() {
  return {
    id: commentID,
    revision,
    author: {
      id: "33333333-3333-4333-8333-333333333333",
      username: "alice",
      displayName: "Alice",
      avatarUrl: "",
    },
    body: "First",
    createdAt: "2026-08-12T01:02:03Z",
    editedAt: null,
    viewerCanUpdate: true,
  };
}
