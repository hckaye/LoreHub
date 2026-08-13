import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { search } from "../src/i18n/dictionaries/search";
import type { CodeSearchResults } from "../src/lib/search";
import {
  lastSearchPage,
  normalizeSearchQuery,
  parseCodeSearchQualifier,
  parseSearchResults,
  searchHref,
  type SearchQuery,
} from "../src/lib/search";

const query: SearchQuery = { q: "render queue", type: "all", page: 1 };

test("search response parser accepts the complete strict contract", () => {
  const response = searchResponse();
  delete (response.issues[0] as Partial<Record<string, unknown>>).sourceBranch;
  delete (response.issues[0] as Partial<Record<string, unknown>>).targetBranch;
  const result = parseSearchResults(response, query);
  assert.ok(result);
  assert.equal(result.issues[0]?.kind, "issue");
  assert.equal(result.pullRequests[0]?.kind, "pull_request");
});

test("search response parser rejects missing, extra, and contradictory data", () => {
  assert.equal(parseSearchResults({ ...searchResponse(), legacy: true }, query), null);

  const missingRepositoryField = searchResponse();
  delete (missingRepositoryField.repositories[0] as Partial<Record<string, unknown>>).loreUrl;
  assert.equal(parseSearchResults(missingRepositoryField, query), null);

  const repositoryWithEngagement = searchResponse();
  Object.assign(repositoryWithEngagement.repositories[0]!, { starCount: 4 });
  assert.equal(parseSearchResults(repositoryWithEngagement, query), null);

  const countMismatch = searchResponse();
  countMismatch.counts.issues = 2;
  assert.equal(parseSearchResults(countMismatch, query), null);

  const pageMismatch = searchResponse();
  pageMismatch.page = 2;
  assert.equal(parseSearchResults(pageMismatch, query), null);

  const wrongKind = searchResponse();
  wrongKind.issues[0]!.kind = "pull_request";
  assert.equal(parseSearchResults(wrongKind, query), null);

  const missingPullBranch = searchResponse();
  delete (missingPullBranch.pullRequests[0] as Partial<Record<string, unknown>>).sourceBranch;
  assert.equal(parseSearchResults(missingPullBranch, query), null);
});

test("typed search rejects results from non-selected categories", () => {
  const response = searchResponse();
  response.organizations = [];
  response.users = [];
  response.issues = [];
  response.pullRequests = [];
  assert.ok(parseSearchResults(response, { ...query, type: "repositories" }));

  response.users = [searchUser()];
  assert.equal(parseSearchResults(response, { ...query, type: "repositories" }), null);
});

test("search query normalization bounds page and defaults array values", () => {
  assert.deepEqual(normalizeSearchQuery({ q: "  alpha  ", type: "issues", page: "100000" }), {
    q: "alpha",
    type: "issues",
    page: 100_000,
  });
  assert.deepEqual(normalizeSearchQuery({ q: ["ignored"], type: ["pulls"], page: ["3"] }), {
    q: "",
    type: "all",
    page: 1,
  });
  assert.equal(normalizeSearchQuery({ type: "unknown", page: "100001" }).page, 1);
  assert.equal(normalizeSearchQuery({ type: "unknown", page: "2" }).type, "all");
});

test("search links encode and retain locale, query, type, and page", () => {
  assert.equal(
    searchHref("ja", { q: "描画 & queue", type: "pulls", page: 3 }),
    "/ja/search?q=%E6%8F%8F%E7%94%BB+%26+queue&type=pulls&page=3",
  );
  const response = searchResponse();
  response.counts.issues = 41;
  assert.equal(lastSearchPage(response, "issues"), 3);
});

test("code search requires a repository qualifier and validates grouped hits", () => {
  assert.deepEqual(parseCodeSearchQualifier("repo:acme/renderer Needle"), {
    owner: "acme",
    repository: "renderer",
    terms: ["needle"],
  });
  assert.equal(parseCodeSearchQualifier("needle"), null);

  const response = searchResponse();
  response.repositories = [];
  response.organizations = [];
  response.users = [];
  response.issues = [];
  response.pullRequests = [];
  response.counts = { repositories: 0, organizations: 0, users: 0, issues: 0, pullRequests: 0, code: 1 };
  response.code = {
    revision: "a".repeat(64),
    files: [{ path: "src/main.ts", matchCount: 1, matches: [{ lineNumber: 4, snippet: "return needle;" }] }],
    truncated: false,
  };
  const parsed = parseSearchResults(response, { q: "repo:acme/renderer needle", type: "code", page: 1 });
  assert.ok(parsed?.code);
  assert.equal(parsed.code.files[0]?.matches[0]?.lineNumber, 4);
});

test("search UI wires tabs, pagination, issues, and pull requests", async () => {
  const [page, tabs, pagination, workItems, sharedRow, list, codeResults] = await Promise.all([
    readFile("src/components/search/search-page.tsx", "utf8"),
    readFile("src/components/search/search-type-tabs.tsx", "utf8"),
    readFile("src/components/search/search-pagination.tsx", "utf8"),
    readFile("src/components/search/search-work-item-results.tsx", "utf8"),
    readFile("src/components/work-items/global-work-item-row.tsx", "utf8"),
    readFile("src/components/work-items/global-work-item-list.tsx", "utf8"),
    readFile("src/components/search/search-code-results.tsx", "utf8"),
  ]);
  assert.match(page, /kind="issues"/);
  assert.match(page, /kind="pulls"/);
  assert.match(page, /SearchCodeResults/);
  assert.match(tabs, /searchHref\(locale, query/);
  assert.match(pagination, /rel="prev"/);
  assert.match(pagination, /rel="next"/);
  assert.match(workItems, /GlobalWorkItemRow/);
  assert.match(list, /GlobalWorkItemRow/);
  assert.doesNotMatch(list, /function WorkItemRow/);
  assert.match(sharedRow, /repositoryPath/);
  assert.match(codeResults, /<mark/);
  assert.match(codeResults, /revision/);
});

test("English and Japanese search dictionaries have identical keys", () => {
  assert.deepEqual(Object.keys(search.en).sort(), Object.keys(search.ja).sort());
});

function searchResponse() {
  return {
    repositories: [searchRepository()],
    organizations: [searchOrganization()],
    users: [searchUser()],
    issues: [searchWorkItem("issue")],
    pullRequests: [searchWorkItem("pull_request")],
    counts: { repositories: 1, organizations: 1, users: 1, issues: 1, pullRequests: 1, code: 0 },
    code: undefined as CodeSearchResults | undefined,
    page: 1,
    perPage: 20,
  };
}

function searchRepository() {
  return {
    id: "repository-1",
    organizationId: "organization-1",
    owner: "acme",
    slug: "renderer",
    displayName: "Renderer",
    description: "Rendering tools",
    visibility: "public" as const,
    loreRepositoryId: "lore-1",
    loreUrl: "lore://renderer",
    defaultBranch: "main",
    homepageUrl: "",
    allowIssues: true,
    allowMergeRequests: true,
    topics: ["rendering"],
    issueCount: 1,
    mergeRequestCount: 1,
    archivedAt: null,
    updatedAt: "2026-08-12T01:02:03Z",
    lifecycleState: "active",
  };
}

function searchOrganization() {
  return {
    id: "organization-1",
    slug: "acme",
    displayName: "Acme",
    description: "Engine tools",
    visibility: "public" as const,
    createdAt: "2026-08-12T01:02:03Z",
    websiteUrl: "",
    contactEmail: "",
    defaultRepositoryVisibility: "private" as const,
    role: "member" as const,
    memberCount: 2,
    repositoryCount: 1,
    teamCount: 1,
  };
}

function searchUser() {
  return { id: "user-1", username: "octo", displayName: "Octo Cat", avatarUrl: "" };
}

function searchWorkItem(kind: "issue" | "pull_request") {
  return {
    id: `${kind}-1`,
    kind,
    repository: { id: "repository-1", owner: "acme", slug: "renderer", displayName: "Renderer" },
    number: 1,
    title: "Improve rendering",
    state: "open" as const,
    isDraft: false,
    author: searchUser(),
    assignees: [searchUser()],
    labels: [{ id: "label-1", name: "rendering", color: "1f883d" }],
    milestone: { number: 1, title: "1.0" },
    commentCount: 2,
    approvalCount: kind === "pull_request" ? 1 : 0,
    sourceBranch: kind === "pull_request" ? "rendering" : "",
    targetBranch: kind === "pull_request" ? "main" : "",
    createdAt: "2026-08-12T01:02:03Z",
    updatedAt: "2026-08-12T02:03:04Z",
  };
}
