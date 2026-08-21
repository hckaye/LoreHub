import assert from "node:assert/strict";
import test from "node:test";

import { ciRunPageSchema, dashboardSchema, repositorySchema } from "../src/lib/core-api-contract";

const repository = {
  id: "repository-1",
  organizationId: "organization-1",
  owner: "owner",
  slug: "project",
  displayName: "Project",
  description: "",
  visibility: "private",
  loreRepositoryId: "lore-1",
  loreUrl: "lores://example.test",
  defaultBranch: "main",
  homepageUrl: "",
  allowIssues: true,
  allowMergeRequests: true,
  topics: [],
  issueCount: 0,
  mergeRequestCount: 0,
  archivedAt: null,
  updatedAt: "2026-08-21T00:00:00Z",
};

test("repository responses require the complete runtime contract", () => {
  assert.equal(repositorySchema.safeParse(repository).success, true);
  assert.equal(repositorySchema.safeParse({ ...repository, visibility: "secret" }).success, false);
  assert.equal(repositorySchema.safeParse({ ...repository, topics: "typescript" }).success, false);
});

test("dashboard and CI page contracts reject malformed nested data", () => {
  const dashboard = {
    repositories: [repository],
    organizations: [],
    notifications: [],
    unreadNotifications: 0,
  };
  assert.equal(dashboardSchema.safeParse(dashboard).success, true);
  assert.equal(dashboardSchema.safeParse({ ...dashboard, unreadNotifications: "0" }).success, false);
  assert.equal(
    ciRunPageSchema.safeParse({ runs: [], totalCount: 0, page: 1, perPage: 50, hasMore: false }).success,
    true,
  );
  assert.equal(
    ciRunPageSchema.safeParse({ runs: [], totalCount: 0, page: 1, perPage: 50, hasMore: "false" }).success,
    false,
  );
});
