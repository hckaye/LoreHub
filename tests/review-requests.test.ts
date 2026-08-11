import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("review requests support users, teams, CSRF, and current-revision status", async () => {
  const [migration, store, handlers, page, component, english, japanese] = await Promise.all([
    readFile("services/api/migrations/000036_merge_request_review_requests.sql", "utf8"),
    readFile("services/api/internal/collab/review_request_store.go", "utf8"),
    readFile("services/api/internal/collab/review_request_handlers.go", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/pulls/[number]/page.tsx", "utf8"),
    readFile("src/components/repositories/pull-request-reviewers.tsx", "utf8"),
    readFile("src/i18n/dictionaries/en.ts", "utf8"),
    readFile("src/i18n/dictionaries/ja.ts", "utf8"),
  ]);

  assert.match(migration, /FOREIGN KEY \(merge_request_id, repository_id\)/);
  assert.match(migration, /FOREIGN KEY \(reviewer_team_id, organization_id\)/);
  assert.match(migration, /removed_at IS NULL/);
  assert.match(store, /source_revision = merge_request\.source_revision/);
  assert.match(store, /team_memberships/);
  assert.match(store, /FOR UPDATE OF request/);
  assert.match(store, /insertAudit/);
  assert.match(store, /insertOutbox/);
  assert.match(handlers, /RequestUserReview/);
  assert.match(handlers, /RequestTeamReview/);
  assert.match(page, /getReviewCandidates/);
  assert.match(page, /getReviewRequests/);
  assert.match(component, /putJson<ReviewRequest>/);
  assert.match(component, /deleteJson<null>/);
  assert.match(component, /csrfToken/);
  assert.match(english, /requestedReviewers: "Reviewers"/);
  assert.match(japanese, /requestedReviewers: "レビュアー"/);
  assert.doesNotMatch(component, /demo|fixture/i);
});
