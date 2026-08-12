import assert from "node:assert/strict";
import test from "node:test";

import { parseRepositoryIssuePage, parseRepositoryMergeRequestPage } from "../src/lib/repository-work-item-contract";
import {
  parseRepositoryIssueQuery,
  parseRepositoryMergeRequestQuery,
  repositoryWorkItemSearchParams,
} from "../src/lib/repository-work-item-query";

test("repository issue pages accept the complete list contract", () => {
  const page = issuePage();
  assert.equal(parseRepositoryIssuePage(page)?.issues[0]?.labels[0]?.name, "bug");
});

test("repository issue pages reject incomplete and inconsistent data", () => {
  const missingLabels = issuePage();
  delete (missingLabels.issues[0] as Partial<Record<string, unknown>>).labels;
  assert.equal(parseRepositoryIssuePage(missingLabels), null);

  assert.equal(parseRepositoryIssuePage({ ...issuePage(), hasNext: true }), null);
  assert.equal(parseRepositoryIssuePage({ ...issuePage(), totalCount: 3 }), null);
});

test("repository pull request pages accept metadata and reject legacy rows", () => {
  const page = pullRequestPage();
  assert.equal(parseRepositoryMergeRequestPage(page)?.mergeRequests[0]?.commentCount, 1);

  const legacy = pullRequestPage();
  delete (legacy.mergeRequests[0] as Partial<Record<string, unknown>>).assignees;
  assert.equal(parseRepositoryMergeRequestPage(legacy), null);
});

test("repository work item search parameters preserve bounded filters", () => {
  const issue = parseRepositoryIssueQuery({
    state: "all",
    q: " renderer ",
    label: ["Bug, needs review", "bug"],
    milestone: "none",
    page: "2",
    per_page: "50",
  });
  assert.deepEqual(issue.labels, ["Bug", "needs review"]);
  assert.equal(issue.q, "renderer");
  assert.equal(
    repositoryWorkItemSearchParams(issue).toString(),
    "state=all&q=renderer&milestone=none&page=2&per_page=50&label=Bug&label=needs+review",
  );

  const pullRequest = parseRepositoryMergeRequestQuery({
    source: " feature/render ",
    target: "main",
    draft: "false",
  });
  assert.equal(pullRequest.source, "feature/render");
  assert.equal(pullRequest.draft, false);
  assert.equal(repositoryWorkItemSearchParams({ draft: true }).toString(), "draft=true");
});

test("repository work item search parameters discard malformed values", () => {
  const issue = parseRepositoryIssueQuery({
    q: "x".repeat(257),
    label: ["x".repeat(101)],
    milestone: "0",
    page: "10001",
    per_page: "101",
  });
  assert.equal(issue.q, undefined);
  assert.deepEqual(issue.labels, []);
  assert.equal(issue.milestone, undefined);
  assert.equal(issue.page, 1);
  assert.equal(issue.perPage, 25);
});

function issuePage() {
  return {
    issues: [
      {
        id: "issue-1",
        number: 1,
        title: "Renderer crash",
        body: "GPU details",
        state: "open",
        author: "alice",
        assignee: "bob",
        assignees: [assignee()],
        labels: [label()],
        milestone: milestone(),
        labelCount: 1,
        commentCount: 2,
        createdAt: "2026-08-12T01:00:00Z",
        updatedAt: "2026-08-12T02:00:00Z",
        closedBy: null,
        closedAt: null,
        viewerCanUpdate: true,
        viewerCanManageLabels: true,
        viewerCanManageMilestone: true,
        viewerCanManageAssignees: true,
      },
    ],
    totalCount: 1,
    openCount: 1,
    closedCount: 0,
    page: 1,
    perPage: 25,
    hasNext: false,
  };
}

function pullRequestPage() {
  return {
    mergeRequests: [
      {
        id: "pull-1",
        number: 2,
        title: "Update renderer",
        body: "GPU details",
        state: "open",
        isDraft: false,
        sourceBranch: "feature/render",
        targetBranch: "main",
        sourceRevision: "a".repeat(64),
        targetRevision: "b".repeat(64),
        author: "alice",
        approvalCount: 1,
        mergedBy: null,
        mergedRevision: null,
        mergedAt: null,
        createdAt: "2026-08-12T01:00:00Z",
        updatedAt: "2026-08-12T02:00:00Z",
        closedAt: null,
        viewerCanUpdate: true,
        viewerCanReview: false,
        labels: [label()],
        assignees: [assignee()],
        milestone: milestone(),
        commentCount: 1,
      },
    ],
    totalCount: 1,
    openCount: 1,
    closedCount: 0,
    mergedCount: 0,
    page: 1,
    perPage: 25,
    hasNext: false,
  };
}

function assignee() {
  return { id: "user-2", username: "bob", displayName: "Bob", avatarUrl: "" };
}

function label() {
  return {
    id: "label-1",
    repositoryId: "repository-1",
    name: "bug",
    description: "A bug",
    color: "D73A4A",
    createdAt: "2026-08-12T00:00:00Z",
  };
}

function milestone() {
  return { id: "milestone-1", number: 1, title: "Release", state: "open", dueOn: null };
}
