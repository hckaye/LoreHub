import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { addReleaseAsset, createRelease, deleteRelease, publishRelease } from "../src/lib/release-client";

test("release mutations pin Lore revisions and preserve CSRF and version checks", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ input, init });
    if (init?.method === "DELETE") return new Response(null, { status: 204 });
    return Response.json({
      id: "release-1",
      revision: "a".repeat(64),
      version: requests.length,
    });
  };
  try {
    await createRelease(
      "Epic Games",
      "Lore Hub",
      {
        tagName: "v1.0.0",
        title: "Version 1",
        notes: "Stable",
        sourceBranch: "main",
        revision: "a".repeat(64),
        state: "draft",
      },
      "csrf-token",
    );
    await publishRelease("Epic Games", "Lore Hub", "release-1", 1, "csrf-token");
    await addReleaseAsset(
      "Epic Games",
      "Lore Hub",
      "release-1",
      { name: "package.zip", externalUrl: "https://example.test/package.zip", expectedVersion: 2 },
      "csrf-token",
    );
    await deleteRelease("Epic Games", "Lore Hub", "release-1", 3, "csrf-token");
  } finally {
    globalThis.fetch = originalFetch;
  }
  assert.equal(requests.length, 4);
  assert.equal(String(requests[0].input), "/api/v1/repositories/Epic%20Games/Lore%20Hub/releases");
  assert.deepEqual(
    requests.map((request) => request.init?.method),
    ["POST", "POST", "POST", "DELETE"],
  );
  for (const request of requests) {
    assert.equal(new Headers(request.init?.headers).get("X-CSRF-Token"), "csrf-token");
  }
  assert.deepEqual(JSON.parse(String(requests[1].init?.body)), { expectedVersion: 1 });
  assert.deepEqual(JSON.parse(String(requests[3].init?.body)), { expectedVersion: 3 });
  assert.equal(JSON.parse(String(requests[0].init?.body)).revision, "a".repeat(64));
});

test("release creation verifies Lore before PostgreSQL persistence", async () => {
  const [handler, store, migration, createForm, page] = await Promise.all([
    readFile("services/api/internal/releases/handlers.go", "utf8"),
    readFile("services/api/internal/releases/store.go", "utf8"),
    readFile("services/api/migrations/000027_repository_releases.sql", "utf8"),
    readFile("src/components/repositories/release-create-form.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/releases/page.tsx", "utf8"),
  ]);
  assert.match(handler, /branch\.LatestRevision == input\.Revision/);
  assert.match(handler, /Scope: loreclient\.ScopeRead/);
  assert.match(handler, /api\.store\.Create/);
  assert.ok(handler.indexOf("verifyRevision") < handler.indexOf("api.store.Create"));
  assert.match(store, /releaseWriteAllowed/);
  assert.match(store, /organization\.active/);
  assert.match(migration, /UNIQUE \(repository_id, tag_name\)/);
  assert.match(migration, /FOREIGN KEY \(release_id, repository_id\)/);
  assert.match(createForm, /revision: selectedBranch\.latestRevision/);
  assert.match(page, /getReleases\(owner, repository, page\)/);
  assert.doesNotMatch(createForm, /\bgit\b/i);
  assert.doesNotMatch(createForm, /demo|fixture/i);
});
