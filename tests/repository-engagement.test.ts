import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const headerPath = "src/components/repositories/repository-header.tsx";

test("repository header persists Star and Watch changes with CSRF mutations", async () => {
  const [header, layout, types] = await Promise.all([
    readFile(headerPath, "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/layout.tsx", "utf8"),
    readFile("src/lib/api-types.ts", "utf8"),
  ]);
  assert.match(header, /putJson<RepositoryEngagement>/);
  assert.match(header, /deleteJson<RepositoryEngagement>/);
  assert.match(header, /session\.csrfToken/);
  assert.match(header, /aria-pressed=/);
  assert.match(header, /brandedAuthUrl/);
  assert.match(layout, /getAuthSession\(\)/);
  assert.match(types, /viewerHasStarred\?: boolean/);
  assert.match(types, /viewerIsWatching\?: boolean/);
});

test("repository engagement API exposes tenant-scoped Star and Watch routes", async () => {
  const [routes, store, migration] = await Promise.all([
    readFile("services/api/internal/collab/routes.go", "utf8"),
    readFile("services/api/internal/collab/repository_engagement_store.go", "utf8"),
    readFile("services/api/migrations/000026_repository_engagement.sql", "utf8"),
  ]);
  assert.match(routes, /PUT "\+base\+"\/star/);
  assert.match(routes, /DELETE "\+base\+"\/watch/);
  assert.match(store, /pgx\.Serializable/);
  assert.match(store, /repository_engagement\.watched/);
  assert.match(migration, /PRIMARY KEY \(repository_id, user_id\)/);
});
