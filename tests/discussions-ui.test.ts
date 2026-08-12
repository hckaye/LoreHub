import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("discussion navigation and routes are exposed", async () => {
  const [routes, header, listPage, detailPage, newPage] = await Promise.all([
    readFile("src/lib/routes.ts", "utf8"),
    readFile("src/components/repositories/repository-header.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/discussions/page.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/discussions/[number]/page.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/discussions/new/page.tsx", "utf8"),
  ]);
  assert.match(routes, /"discussions"/);
  assert.match(header, /MessageSquare/);
  assert.match(listPage, /getDiscussions/);
  assert.match(detailPage, /getDiscussion/);
  assert.match(detailPage, /comment_page/);
  assert.match(newPage, /DiscussionForm/);
});

test("discussion client surfaces use the authenticated mutation API", async () => {
  const [api, detail, heading, form, categories] = await Promise.all([
    readFile("src/lib/lorehub-api.ts", "utf8"),
    readFile("src/components/discussions/discussion-detail.tsx", "utf8"),
    readFile("src/components/discussions/discussion-heading.tsx", "utf8"),
    readFile("src/components/discussions/discussion-form.tsx", "utf8"),
    readFile("src/components/discussions/discussion-category-settings.tsx", "utf8"),
  ]);
  assert.match(api, /getDiscussionCategories/);
  assert.match(api, /getDiscussions/);
  assert.match(api, /getDiscussion\(/);
  for (const marker of ["putJson", "deleteJson", "patchJson", "postJson"]) {
    assert.match(detail, new RegExp(marker));
  }
  assert.match(detail, /deleteDiscussionConfirm/);
  assert.match(heading, /viewerCanVote/);
  assert.match(detail, /viewerCanComment/);
  assert.match(form, /postJson/);
  assert.match(categories, /patchJson/);
  assert.match(categories, /deleteJson/);
});

test("English and Japanese discussion copy is present", async () => {
  const [english, japanese] = await Promise.all([
    readFile("src/i18n/dictionaries/discussions.ts", "utf8"),
    readFile("src/i18n/dictionaries/ja.ts", "utf8"),
  ]);
  assert.match(english, /newDiscussion/);
  assert.match(english, /categoriesTitle/);
  assert.match(japanese, /discussions\.ja/);
});
