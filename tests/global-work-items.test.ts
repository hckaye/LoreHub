import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { globalWorkItemQuery, normalizeGlobalWorkItemPage } from "../src/lib/global-work-items";

const item = {
  id: "work-item-1",
  kind: "issue",
  repository: { id: "repository-1", owner: "acme", slug: "game", displayName: "Game" },
  number: 7,
  title: "Renderer crash",
  state: "open",
  isDraft: false,
  author: { id: "user-1", username: "alice", displayName: "Alice", avatarUrl: "" },
  assignees: [],
  labels: [{ id: "label-1", name: "bug", color: "D73A4A" }],
  milestone: null,
  commentCount: 2,
  approvalCount: 0,
  createdAt: "2026-08-12T00:00:00Z",
  updatedAt: "2026-08-12T01:00:00Z",
};

test("global work item pages accept the API response shape", () => {
  assert.deepEqual(normalizeGlobalWorkItemPage({ items: [item], nextCursor: "next" }), {
    items: [{ ...item, sourceBranch: "", targetBranch: "" }],
    nextCursor: "next",
  });
  assert.deepEqual(normalizeGlobalWorkItemPage({ items: [item], nextCursor: null, openCount: 4, closedCount: 2 }), {
    items: [{ ...item, sourceBranch: "", targetBranch: "" }],
    nextCursor: null,
    openCount: 4,
    closedCount: 2,
  });
});

test("global work item pages reject invalid nested values", () => {
  assert.equal(
    normalizeGlobalWorkItemPage({ items: [{ ...item, labels: [{ id: "label-1", name: "bug", color: "red" }] }] }),
    null,
  );
  assert.equal(normalizeGlobalWorkItemPage({ items: [{ ...item, author: { username: "alice" } }] }), null);
});

test("global work item pages reject invalid pagination values", () => {
  assert.equal(normalizeGlobalWorkItemPage({ items: [item], nextCursor: 42 }), null);
  assert.equal(normalizeGlobalWorkItemPage({ items: "invalid" }), null);
});

test("global work item queries keep page 1 unless a cursor is present", () => {
  assert.equal(globalWorkItemQuery("issue", {}).page, 1);
  assert.equal(globalWorkItemQuery("issue", { page: "3" }).page, 1);
  assert.equal(globalWorkItemQuery("issue", { cursor: "abc", page: "3" }).page, 3);
  assert.equal(globalWorkItemQuery("issue", { cursor: "abc" }).page, 2);
});

test("global work item dashboards match GitHub's issues dashboard chrome", async () => {
  const [list, row, css, copy] = await Promise.all([
    readFile("src/components/work-items/global-work-item-list.tsx", "utf8"),
    readFile("src/components/work-items/global-work-item-row.tsx", "utf8"),
    readFile("src/components/work-items/global-work-item-list.module.css", "utf8"),
    readFile("src/i18n/dictionaries/global-work-items.ts", "utf8"),
  ]);
  assert.doesNotMatch(list, /styles\.create/);
  assert.doesNotMatch(list, /\/new`/);
  assert.doesNotMatch(list, /styles\.heading/);
  assert.match(list, /styles\.boxHeader/);
  assert.match(list, /openWithCount/);
  assert.match(list, /closedWithCount/);
  assert.match(list, /CircleDot/);
  assert.match(list, /<Check /);
  assert.match(list, /paginationCurrent/);
  assert.match(css, /max-width: 1216px/);
  assert.match(css, /padding: 24px 16px/);
  assert.match(css, /background: var\(--accent-emphasis\)/);
  assert.match(row, /itemMetadata/);
  assert.match(row, /size=\{16\}/);
  assert.match(copy, /\{org\}\/\{repo\}#\{number\} opened \{time\} by \{author\}/);
  assert.match(copy, /\{org\}\/\{repo\}#\{number\} · \{time\}に\{author\}が作成/);
});

test("global work item routes use the authenticated API and primary navigation", async () => {
  const [issuesPage, pullsPage, newIssuePage, newPullPage, header, api, english, japanese] = await Promise.all([
    readFile("src/app/[locale]/issues/page.tsx", "utf8"),
    readFile("src/app/[locale]/pulls/page.tsx", "utf8"),
    readFile("src/app/[locale]/issues/new/page.tsx", "utf8"),
    readFile("src/app/[locale]/pulls/new/page.tsx", "utf8"),
    readFile("src/components/layout/site-header.tsx", "utf8"),
    readFile("src/lib/lorehub-api.ts", "utf8"),
    readFile("src/i18n/dictionaries/en.ts", "utf8"),
    readFile("src/i18n/dictionaries/ja.ts", "utf8"),
  ]);
  assert.match(issuesPage, /getGlobalIssues\(query\)/);
  assert.match(pullsPage, /getGlobalPullRequests\(query\)/);
  assert.match(newIssuePage, /dashboard\.data\.repositories/);
  assert.match(newPullPage, /dashboard\.data\.repositories/);
  assert.match(header, /href=\{`\/\$\{locale\}\/issues`\}/);
  assert.match(header, /href=\{`\/\$\{locale\}\/pulls`\}/);
  assert.match(api, /getGlobalWorkItems\("issues", query\)/);
  assert.match(api, /getGlobalWorkItems\("pulls", query\)/);
  assert.match(api, /request<unknown>\(`\/api\/v1\/\$\{resource\}\?\$\{search\}`\)/);
  assert.match(english, /globalWorkItems: globalWorkItems\.en/);
  assert.match(japanese, /globalWorkItems: globalWorkItems\.ja/);
});
