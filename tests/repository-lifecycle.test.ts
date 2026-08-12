import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { archiveRepository, unarchiveRepository } from "../src/lib/repository-lifecycle-client";

test("repository archive mutations encode the path and send confirmation with CSRF", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ input, init });
    return Response.json({ id: "repository-1", archivedAt: new Date().toISOString() });
  };
  try {
    const archived = await archiveRepository("acme studio", "game/client", "acme studio/game/client", "csrf");
    const unarchived = await unarchiveRepository("acme studio", "game/client", "acme studio/game/client", "csrf");
    assert.equal(archived.ok, true);
    assert.equal(unarchived.ok, true);
    assert.equal(requests[0]?.input, "/api/v1/repositories/acme%20studio/game%2Fclient/archive");
    assert.equal(requests[0]?.init?.method, "PUT");
    assert.equal(requests[1]?.init?.method, "DELETE");
    for (const request of requests) {
      assert.equal(new Headers(request.init?.headers).get("X-CSRF-Token"), "csrf");
      assert.equal(request.init?.body, JSON.stringify({ confirmation: "acme studio/game/client" }));
    }
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("repository archive UI exposes read-only state and hides creation links", async () => {
  const [header, settings, issues, pulls, chooser] = await Promise.all([
    readFile("src/components/repositories/repository-header.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/settings/page.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/issues/page.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/pulls/page.tsx", "utf8"),
    readFile("src/components/repositories/repository-chooser.tsx", "utf8"),
  ]);
  assert.match(header, /repository\.archivedAt/);
  assert.match(header, /repositoryLifecycle\.banner/);
  assert.match(settings, /data\.archivedAt/);
  assert.match(settings, /RepositoryLifecycleSettings/);
  assert.match(issues, /!archived/);
  assert.match(pulls, /archived \? undefined/);
  assert.match(chooser, /filter\(\(repository\) => !repository\.archivedAt\)/);
});
