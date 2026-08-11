import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { assignIssueUser, removeIssueUser, searchIssueAssignees } from "../src/lib/issue-assignee-client";

test("assignee mutations encode paths and include CSRF protection", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ input, init });
    if (init?.method === "DELETE") return new Response(null, { status: 204 });
    if (!init?.method) return Response.json({ items: [], hasMore: false });
    return Response.json({
      id: "user-1",
      username: "Alex/Smith",
      displayName: "Alex Smith",
      avatarUrl: "",
    });
  };
  try {
    await searchIssueAssignees("Epic Games", "Lore Hub", "Alex/Smith");
    await assignIssueUser("Epic Games", "Lore Hub", 9, "Alex/Smith", "csrf-token");
    await removeIssueUser("Epic Games", "Lore Hub", 9, "Alex/Smith", "csrf-token");
  } finally {
    globalThis.fetch = originalFetch;
  }

  const path = "/api/v1/repositories/Epic%20Games/Lore%20Hub/issues/9/assignees/Alex%2FSmith";
  assert.deepEqual(
    requests.map((request) => [String(request.input), request.init?.method]),
    [
      [`/api/v1/repositories/Epic%20Games/Lore%20Hub/assignees?limit=100&query=Alex%2FSmith`, undefined],
      [path, "PUT"],
      [path, "DELETE"],
    ],
  );
  assert.equal(requests[0].init?.credentials, "include");
  for (const request of requests.slice(1)) {
    const headers = new Headers(request.init?.headers);
    assert.equal(headers.get("X-CSRF-Token"), "csrf-token");
    assert.equal(request.init?.credentials, "include");
  }
});

test("assignees are repository-scoped and connected to Issue pages", async () => {
  const [migration, store, handlers, page, sidebar] = await Promise.all([
    readFile("services/api/migrations/000029_issue_assignees.sql", "utf8"),
    readFile("services/api/internal/collab/issue_assignee_store.go", "utf8"),
    readFile("services/api/internal/collab/issue_assignee_handlers.go", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/issues/[number]/page.tsx", "utf8"),
    readFile("src/components/repositories/issue-sidebar.tsx", "utf8"),
  ]);
  assert.match(migration, /FOREIGN KEY \(issue_id, repository_id\)/);
  assert.match(migration, /INSERT INTO issue_assignees/);
  assert.match(migration, /DROP COLUMN assignee_id/);
  assert.match(store, /organization\.active/);
  assert.match(store, /actor\.status = 'active'/);
  assert.match(store, /maxIssueAssignees = 10/);
  assert.match(store, /issue\.assignee\.add/);
  assert.match(store, /issue\.assignees\.updated/);
  assert.match(handlers, /PermTriage/);
  assert.match(page, /getAssignableUsers\(owner, repository\)/);
  assert.match(sidebar, /viewerCanManageAssignees/);
  assert.match(sidebar, /searchIssueAssignees/);
  assert.doesNotMatch(page + sidebar, /demo|fixture/i);
});
