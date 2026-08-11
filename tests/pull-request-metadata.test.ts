import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("pull request metadata uses persisted values and protected mutations", async () => {
  const [migration, store, handlers, page, component, dictionary] = await Promise.all([
    readFile("services/api/migrations/000037_merge_request_metadata.sql", "utf8"),
    readFile("services/api/internal/collab/merge_request_metadata_store.go", "utf8"),
    readFile("services/api/internal/collab/merge_request_metadata_handlers.go", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/pulls/[number]/page.tsx", "utf8"),
    readFile("src/components/repositories/pull-request-metadata.tsx", "utf8"),
    readFile("src/i18n/dictionaries/pull-request-metadata.ts", "utf8"),
  ]);

  assert.match(migration, /FOREIGN KEY \(merge_request_id, repository_id\)/);
  assert.match(migration, /FOREIGN KEY \(milestone_id, repository_id\)/);
  assert.match(store, /FOR UPDATE OF merge_request/);
  assert.match(store, /insertAudit/);
  assert.match(store, /insertOutbox/);
  assert.match(handlers, /requireMutationActor/);
  assert.match(page, /getMergeRequestMetadata/);
  assert.match(component, /putJson/);
  assert.match(component, /deleteJson/);
  assert.match(component, /csrfToken/);
  assert.match(dictionary, /en:/);
  assert.match(dictionary, /ja:/);
  assert.doesNotMatch(component, /demo|fixture/i);
});
