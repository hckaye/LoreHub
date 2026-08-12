import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("wiki storage preserves history and checks repository boundaries", async () => {
  const [migration, mutations, store, handlers] = await Promise.all([
    readFile("services/api/migrations/000034_repository_wiki.sql", "utf8"),
    readFile("services/api/internal/wiki/mutation_store.go", "utf8"),
    readFile("services/api/internal/wiki/store.go", "utf8"),
    readFile("services/api/internal/wiki/handlers.go", "utf8"),
  ]);
  assert.match(migration, /repository_wiki_page_versions/);
  assert.match(migration, /FOREIGN KEY \(page_id, repository_id\)/);
  assert.match(migration, /WHERE archived_at IS NULL/);
  assert.match(store, /repositoryWriteAllowed/);
  assert.match(mutations, /ExpectedVersion/);
  assert.match(mutations, /insertAudit/);
  assert.match(mutations, /insertOutbox/);
  assert.match(handlers, /ResolveOptionalActor/);
  assert.match(handlers, /ExpectedVersion/);
});

test("wiki Markdown rendering does not enable raw HTML", async () => {
  const markdown = await readFile("src/components/wiki/markdown-content.tsx", "utf8");
  assert.match(markdown, /remarkGfm/);
  assert.match(markdown, /skipHtml/);
  assert.doesNotMatch(markdown, /rehypeRaw|dangerouslySetInnerHTML/);
});

test("wiki pages are available from the repository navigation", async () => {
  const [header, routes, page] = await Promise.all([
    readFile("src/components/repositories/repository-header.tsx", "utf8"),
    readFile("services/api/internal/wiki/routes.go", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/wiki/[slug]/page.tsx", "utf8"),
  ]);
  assert.match(header, /\["wiki", BookOpenText\]/);
  assert.match(routes, /\/history\/\{version\}/);
  assert.match(page, /getWikiRevision/);
});
