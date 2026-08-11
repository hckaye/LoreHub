import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("repository insights enforce visibility and repository-scoped aggregation", async () => {
  const [store, handler, migration] = await Promise.all([
    readFile("services/api/internal/platform/repository_insights_store.go", "utf8"),
    readFile("services/api/internal/httpapi/repository_insights_http.go", "utf8"),
    readFile("services/api/migrations/000032_repository_insights.sql", "utf8"),
  ]);
  assert.match(store, /repositoryAccessClause/);
  assert.match(store, /repository_id = \$1/);
  assert.match(store, /action = 'branch\.push'/);
  assert.match(handler, /private, no-store/);
  assert.match(handler, /parsed != 7 && parsed != 30 && parsed != 90/);
  assert.match(migration, /audit_events \(repository_id, occurred_at DESC, id DESC\)/);
});

test("repository insights replace the empty screen with activity and paired locale copy", async () => {
  const [page, component, api, english, japanese] = await Promise.all([
    readFile("src/app/[locale]/[owner]/[repository]/insights/page.tsx", "utf8"),
    readFile("src/components/repositories/repository-insights.tsx", "utf8"),
    readFile("src/lib/lorehub-api.ts", "utf8"),
    readFile("src/i18n/dictionaries/en.ts", "utf8"),
    readFile("src/i18n/dictionaries/ja.ts", "utf8"),
  ]);
  assert.match(page, /getRepositoryInsights/);
  assert.match(component, /RepositoryPanel/);
  assert.match(component, /<progress/);
  assert.match(component, /contributors\.map/);
  assert.match(api, /repositories\/\$\{encodeURIComponent\(owner\)\}.*\/insights/);
  assert.match(english, /insightsPage: insights\.en/);
  assert.match(japanese, /insightsPage: insights\.ja/);
});
