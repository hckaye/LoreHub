import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const files = {
  migration: "services/api/migrations/000060_comment_reactions.sql",
  store: "services/api/internal/collab/reaction_store.go",
  routes: "services/api/internal/collab/routes.go",
  issueConversation: "src/components/repositories/issue-conversation.tsx",
  pullRequestConversation: "src/components/repositories/pull-request-conversation.tsx",
  reactionBar: "src/components/repositories/reaction-bar.tsx",
  english: "src/i18n/dictionaries/en.ts",
  japanese: "src/i18n/dictionaries/ja.ts",
} as const;

test("comment reactions cover the full stack and GitHub reaction set", async () => {
  const source = Object.fromEntries(
    await Promise.all(Object.entries(files).map(async ([name, path]) => [name, await readFile(path, "utf8")])),
  ) as Record<keyof typeof files, string>;
  assert.match(source.migration, /CREATE TABLE comment_reactions/u);
  assert.match(source.migration, /UNIQUE \(subject_kind, subject_id, username, reaction\)/u);
  assert.match(source.store, /ON CONFLICT \(subject_kind, subject_id, username, reaction\)/u);
  assert.match(source.store, /GROUP BY subject_id, reaction/u);
  assert.match(source.store, /bool_or\(username = \$4\)/u);
  assert.ok(source.routes.includes('mux.HandleFunc("PUT "+base+"/reactions"'));
  assert.ok(source.routes.includes('mux.HandleFunc("DELETE "+base+"/reactions"'));
  assert.match(source.issueConversation, /subjectKind="issue"/u);
  assert.match(source.issueConversation, /subjectKind="issue_comment"/u);
  assert.match(source.pullRequestConversation, /subjectKind="merge_request"/u);
  assert.match(source.pullRequestConversation, /subjectKind="merge_request_comment"/u);
  assert.match(source.reactionBar, /deleteJsonWithBody/u);
  assert.match(source.reactionBar, /putJson/u);
  assert.match(source.reactionBar, /setLocalState\(\{ source, values: previous \}\)/u);
  assert.match(source.reactionBar, /csrfToken/u);
  for (const reaction of ["+1", "-1", "laugh", "confused", "heart", "hooray", "rocket", "eyes"]) {
    assert.ok(source.migration.includes(`'${reaction}'`));
    assert.ok(source.reactionBar.includes(reaction));
  }
  for (const dictionary of [source.english, source.japanese]) {
    assert.match(dictionary, /reactions:/u);
    assert.match(dictionary, /failed:/u);
  }
});
