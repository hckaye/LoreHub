import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("branch management uses Lore revisions and CSRF-protected mutations", async () => {
  const [page, management, createForm, ruleEditor, serverAPI] = await Promise.all([
    readFile("src/app/[locale]/[owner]/[repository]/branches/page.tsx", "utf8"),
    readFile("src/components/repositories/branch-management.tsx", "utf8"),
    readFile("src/components/repositories/branch-create-form.tsx", "utf8"),
    readFile("src/components/repositories/branch-rule-editor.tsx", "utf8"),
    readFile("src/lib/lorehub-api.ts", "utf8"),
  ]);
  assert.match(page, /getBranchOverview\(owner, slug\)/);
  assert.match(page, /getBranchRules\(owner, slug\)/);
  assert.match(serverAPI, /\/branch-rules/);
  assert.match(createForm, /sourceRevision: source\.latestRevision/);
  assert.match(createForm, /session\.csrfToken/);
  assert.match(management, /branch\.name === props\.repository\.defaultBranch/);
  assert.match(management, /protectedBranches\.has\(branch\.id\)/);
  assert.match(management, /deleteJson<null>/);
  assert.match(ruleEditor, /postJson<BranchRule>/);
  assert.match(ruleEditor, /patchJson<BranchRule>/);
  assert.match(ruleEditor, /deleteJson<null>/);
});

test("branch operations remain in Lore and branch protection remains in PostgreSQL", async () => {
  const [sdk, routes, store, hook] = await Promise.all([
    readFile("services/api/internal/lore/sdk_branch.go", "utf8"),
    readFile("services/api/internal/branches/api.go", "utf8"),
    readFile("services/api/internal/platform/content_store.go", "utf8"),
    readFile("infra/lore/lorehub_policy_hook.rs", "utf8"),
  ]);
  assert.match(sdk, /loresdk\.BranchCreate/);
  assert.match(sdk, /loresdk\.BranchPush/);
  assert.match(sdk, /loresdk\.BranchArchive/);
  assert.match(routes, /ScopeWrite/);
  assert.match(routes, /source\.LatestRevision != input\.SourceRevision/);
  assert.match(store, /RecordLoreBranchCreation/);
  assert.match(store, /branch\.created/);
  assert.match(hook, /HookPoint::BranchCreate => "branch_create"/);
});
