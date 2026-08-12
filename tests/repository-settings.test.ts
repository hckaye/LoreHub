import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { updateRepositorySettings } from "../src/lib/repository-settings-client";

test("repository settings encode paths and preserve CSRF", async () => {
  const originalFetch = globalThis.fetch;
  const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ input, init });
    return Response.json({ id: "repository-1" });
  };
  try {
    await updateRepositorySettings(
      "Epic Games",
      "Lore Hub",
      {
        displayName: "Lore Hub",
        description: "Lore collaboration",
        homepageUrl: "https://example.test/lore",
        topics: ["game-development", "lore"],
        visibility: "private",
      },
      "csrf-token",
    );
  } finally {
    globalThis.fetch = originalFetch;
  }

  assert.equal(requests.length, 1);
  assert.equal(String(requests[0].input), "/api/v1/repositories/Epic%20Games/Lore%20Hub/settings");
  assert.equal(requests[0].init?.method, "PATCH");
  assert.equal(new Headers(requests[0].init?.headers).get("X-CSRF-Token"), "csrf-token");
  assert.deepEqual(JSON.parse(String(requests[0].init?.body)), {
    displayName: "Lore Hub",
    description: "Lore collaboration",
    homepageUrl: "https://example.test/lore",
    topics: ["game-development", "lore"],
    visibility: "private",
  });
});

test("repository settings use effective admin access and the production form", async () => {
  const [store, handler, page, home, search, form, card, header, topicList, english, japanese] = await Promise.all([
    readFile("services/api/internal/platform/organization_store.go", "utf8"),
    readFile("services/api/internal/httpapi/organization_http.go", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/settings/page.tsx", "utf8"),
    readFile("src/app/[locale]/page.tsx", "utf8"),
    readFile("src/app/[locale]/search/page.tsx", "utf8"),
    readFile("src/components/repositories/repository-settings-form.tsx", "utf8"),
    readFile("src/components/repositories/repository-card.tsx", "utf8"),
    readFile("src/components/repositories/repository-about.tsx", "utf8"),
    readFile("src/components/repositories/repository-topic-list.tsx", "utf8"),
    readFile("src/i18n/dictionaries/en.ts", "utf8"),
    readFile("src/i18n/dictionaries/ja.ts", "utf8"),
  ]);
  assert.match(store, /team_repository_roles/);
  assert.match(store, /role\.role = 'admin'/);
  assert.match(store, /repository\.settings_update/);
  assert.match(store, /repository\.settings_updated/);
  assert.match(handler, /RepositoryForSettings/);
  assert.match(page, /getRepositorySettings/);
  assert.match(page, /<RepositorySettingsForm/);
  assert.match(form, /updateRepositorySettings/);
  assert.match(form, /maxLength=\{10_000\}/);
  assert.match(form, /topicsInvalid/);
  assert.match(home, /\.\.\.repository\.topics/);
  assert.match(search, /getSearchResults\(query\.q, query\.type, query\.page\)/);
  assert.match(card, /limit=\{5\}/);
  assert.match(header, /<RepositoryTopicList/);
  assert.match(topicList, /type=repositories/);
  assert.doesNotMatch(page, /readOnly|canonicalNote/);
  assert.doesNotMatch(english, /Lore data remains read-only here/);
  assert.doesNotMatch(japanese, /Loreのデータはここでは読み取り専用です/);
});
