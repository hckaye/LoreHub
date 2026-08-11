import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  assignIssueMilestone,
  createMilestone,
  deleteMilestone,
  removeIssueMilestone,
  updateMilestone,
} from "../src/lib/milestone-client";

test("milestone mutations encode paths and preserve CSRF and version checks", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ input, init });
    if (init?.method === "DELETE") return new Response(null, { status: 204 });
    return Response.json({ id: "milestone-1", number: 3, version: requests.length });
  };
  try {
    await createMilestone(
      "Epic Games",
      "Lore Hub",
      { title: "Version 2", description: "Scope", dueOn: "2026-12-31" },
      "csrf-token",
    );
    await updateMilestone(
      "Epic Games",
      "Lore Hub",
      3,
      { dueOn: null, state: "closed", expectedVersion: 1 },
      "csrf-token",
    );
    await assignIssueMilestone("Epic Games", "Lore Hub", 9, 3, "csrf-token");
    await removeIssueMilestone("Epic Games", "Lore Hub", 9, "csrf-token");
    await deleteMilestone("Epic Games", "Lore Hub", 3, 2, "csrf-token");
  } finally {
    globalThis.fetch = originalFetch;
  }

  const base = "/api/v1/repositories/Epic%20Games/Lore%20Hub";
  assert.deepEqual(
    requests.map((request) => [String(request.input), request.init?.method]),
    [
      [`${base}/milestones`, "POST"],
      [`${base}/milestones/3`, "PATCH"],
      [`${base}/issues/9/milestone/3`, "PUT"],
      [`${base}/issues/9/milestone`, "DELETE"],
      [`${base}/milestones/3`, "DELETE"],
    ],
  );
  for (const request of requests) {
    assert.equal(new Headers(request.init?.headers).get("X-CSRF-Token"), "csrf-token");
  }
  assert.deepEqual(JSON.parse(String(requests[1].init?.body)), {
    dueOn: null,
    state: "closed",
    expectedVersion: 1,
  });
  assert.deepEqual(JSON.parse(String(requests[4].init?.body)), { expectedVersion: 2 });
});

test("milestones keep repository boundaries and connect to issues", async () => {
  const [migration, store, handlers, page, issuePage, sidebar] = await Promise.all([
    readFile("services/api/migrations/000028_repository_milestones.sql", "utf8"),
    readFile("services/api/internal/milestones/store.go", "utf8"),
    readFile("services/api/internal/milestones/handlers.go", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/milestones/page.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/issues/[number]/page.tsx", "utf8"),
    readFile("src/components/repositories/issue-sidebar.tsx", "utf8"),
  ]);
  assert.match(migration, /FOREIGN KEY \(milestone_id, repository_id\)/);
  assert.match(migration, /UNIQUE \(repository_id, number\)/);
  assert.match(store, /organization\.active/);
  assert.match(store, /actor\.status = 'active'/);
  assert.match(store, /membership\.role = ANY/);
  assert.match(handlers, /collab\.PermTriage/);
  assert.match(handlers, /ExpectedVersion/);
  assert.match(page, /getMilestones\(owner, repository, state, page\)/);
  assert.match(issuePage, /getMilestones\(owner, repository, "all", 1, 100\)/);
  assert.match(sidebar, /viewerCanManageMilestone/);
  assert.doesNotMatch(page + sidebar, /demo|fixture/i);
});
