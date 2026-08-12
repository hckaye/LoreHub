import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { parseRequiredStatusChecks } from "../src/components/repositories/branch-rule-input";
import { parseBranchRule } from "../src/lib/branch-rule-contract";
import { createRevisionStatus, parseRevisionStatusPage, revisionStatusPath } from "../src/lib/commit-status-client";
import { parseMergeReadiness } from "../src/lib/merge-readiness-contract";

const revision = "a".repeat(64);

test("revision status parser accepts the complete current contract", () => {
  const page = parseRevisionStatusPage(statusPage());
  assert.ok(page);
  assert.equal(page.state, "success");
  assert.equal(page.statuses[0]?.creator.username, "octo");
  assert.equal(page.history[0]?.context, "build");
});

test("revision status parser rejects missing, legacy, and extra fields", () => {
  const missingCreatorID = statusPage();
  delete (missingCreatorID.statuses[0]?.creator as Partial<Record<string, unknown>>).id;
  assert.equal(parseRevisionStatusPage(missingCreatorID), null);

  const legacyTimestamp = statusPage();
  const status = legacyTimestamp.statuses[0] as Record<string, unknown>;
  status.updatedAt = status.createdAt;
  assert.equal(parseRevisionStatusPage(legacyTimestamp), null);

  const invalidState = statusPage();
  invalidState.state = "complete";
  assert.equal(parseRevisionStatusPage(invalidState), null);
});

test("revision status creation sends the typed contract and validates its response", async () => {
  const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
  const previousFetch = globalThis.fetch;
  globalThis.fetch = async (input, init) => {
    requests.push({ input, init });
    return Response.json(statusRecord(), { status: 201 });
  };
  try {
    const result = await createRevisionStatus(
      "acme team",
      "game/client",
      revision,
      {
        state: "success",
        context: "build",
        description: "Build passed",
        targetUrl: "https://ci.example/runs/1",
        idempotencyKey: "run-1",
      },
      "csrf-token",
    );
    assert.equal(result.ok, true);
    assert.equal(requests[0]?.input, revisionStatusPath("acme team", "game/client", revision));
    assert.equal(requests[0]?.init?.method, "POST");
    assert.equal(new Headers(requests[0]?.init?.headers).get("X-CSRF-Token"), "csrf-token");
    assert.deepEqual(JSON.parse(String(requests[0]?.init?.body)), {
      state: "success",
      context: "build",
      description: "Build passed",
      targetUrl: "https://ci.example/runs/1",
      idempotencyKey: "run-1",
    });
  } finally {
    globalThis.fetch = previousFetch;
  }
});

test("required status contexts are trimmed, deduplicated, and bounded", () => {
  assert.deepEqual(parseRequiredStatusChecks(" build \nBUILD\ntest / linux\n"), {
    ok: true,
    checks: ["build", "test / linux"],
  });
  assert.deepEqual(parseRequiredStatusChecks(`${"x".repeat(101)}\nbuild`), {
    ok: false,
    error: "too_long",
  });
  assert.deepEqual(parseRequiredStatusChecks(Array.from({ length: 51 }, (_, index) => `check-${index}`).join("\n")), {
    ok: false,
    error: "too_many",
  });
  assert.deepEqual(parseRequiredStatusChecks("build\u0000"), { ok: false, error: "invalid" });
});

test("branch rule parser requires the status context array", () => {
  const rule = branchRuleRecord();
  assert.deepEqual(parseBranchRule(rule)?.requiredStatusChecks, ["build", "test / linux"]);
  const legacyRule = branchRuleRecord();
  delete (legacyRule as Partial<typeof legacyRule>).requiredStatusChecks;
  assert.equal(parseBranchRule(legacyRule), null);
  const duplicateRule = branchRuleRecord();
  duplicateRule.requiredStatusChecks = ["build", "BUILD"];
  assert.equal(parseBranchRule(duplicateRule), null);
});

test("merge readiness rejects the previous contract without status checks", () => {
  const current = mergeReadinessRecord();
  assert.equal(parseMergeReadiness(current)?.statusChecks[0]?.required, true);
  const previous = { ...current } as Partial<typeof current>;
  delete previous.statusChecks;
  assert.equal(parseMergeReadiness(previous), null);
  assert.equal(parseMergeReadiness({ ...current, rules: [{}] }), null);
  const extraStatusField = mergeStatusCheck() as ReturnType<typeof mergeStatusCheck> & { legacy?: boolean };
  extraStatusField.legacy = true;
  assert.equal(parseMergeReadiness({ statusChecks: [extraStatusField], rules: current.rules }), null);
});

test("commit and pull request pages render checks without legacy fallbacks", async () => {
  const [commitPage, pullPage, pullDetail, ruleEditor, ruleFields, apiTypes, serverAPI] = await Promise.all([
    readFile("src/app/[locale]/[owner]/[repository]/commit/page.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/pulls/[number]/page.tsx", "utf8"),
    readFile("src/components/repositories/pull-request-detail.tsx", "utf8"),
    readFile("src/components/repositories/branch-rule-editor.tsx", "utf8"),
    readFile("src/components/repositories/branch-rule-fields.tsx", "utf8"),
    readFile("src/lib/api-types.ts", "utf8"),
    readFile("src/lib/lorehub-api.ts", "utf8"),
  ]);
  assert.match(commitPage, /getRevisionStatuses\(owner, slug, revision\.data\.revision\)/);
  assert.match(commitPage, /result\.data\.statuses/);
  assert.match(pullPage, /readinessUnavailableReason/);
  assert.match(pullDetail, /readiness\.statusChecks/);
  assert.match(pullDetail, /case "required_status_checks"/);
  assert.match(ruleEditor, /normalizeBranchRuleInput/);
  assert.match(ruleEditor, /parseBranchRule/);
  assert.match(ruleFields, /RequiredStatusChecksField/);
  assert.match(apiTypes, /requiredStatusChecks: string\[\]/);
  assert.match(serverAPI, /parseMergeReadiness\(result\.data\)/);
  assert.doesNotMatch(pullDetail, /statusChecks \?\? \[\]/);
});

function statusPage() {
  const status = statusRecord();
  return {
    revision,
    state: "success",
    statuses: [{ ...status, creator: { ...status.creator } }],
    history: [{ ...status, creator: { ...status.creator } }],
    page: 1,
    perPage: 30,
    totalCount: 1,
    hasNext: false,
  };
}

function statusRecord() {
  return {
    id: "status-1",
    revision,
    context: "build",
    state: "success",
    description: "Build passed",
    targetUrl: "https://ci.example/runs/1",
    creator: {
      id: "user-1",
      username: "octo",
      displayName: "Octo Cat",
      avatarUrl: "https://avatars.example/octo.png",
    },
    createdAt: "2026-08-12T01:02:03Z",
  };
}

function branchRuleRecord() {
  return {
    id: "rule-1",
    repositoryId: "repository-1",
    pattern: "main",
    requiredApprovals: 1,
    requireCiSuccess: true,
    requiredStatusChecks: ["build", "test / linux"],
    blockDirectPush: true,
    createdAt: "2026-08-12T01:02:03Z",
    updatedAt: "2026-08-12T01:02:03Z",
  };
}

function mergeStatusCheck() {
  return {
    context: "build",
    state: "success",
    description: "Build passed",
    targetUrl: "https://ci.example/runs/1",
    creator: "octo",
    updatedAt: "2026-08-12T01:02:03Z",
    required: true,
  };
}

function mergeReadinessRecord() {
  return {
    mergeRequest: {
      id: "pull-1",
      number: 1,
      title: "Ship",
      body: "",
      state: "open",
      isDraft: false,
      sourceBranch: "feature",
      targetBranch: "main",
      sourceRevision: revision,
      targetRevision: "b".repeat(64),
      author: "octo",
      approvalCount: 1,
      mergedBy: null,
      mergedRevision: null,
      mergedAt: null,
      createdAt: "2026-08-12T01:02:03Z",
      updatedAt: "2026-08-12T01:02:03Z",
      closedAt: null,
      viewerCanUpdate: true,
      viewerCanReview: true,
    },
    currentSourceRevision: revision,
    currentTargetRevision: "b".repeat(64),
    sourceStale: false,
    targetStale: false,
    canMerge: true,
    ready: true,
    blockers: [],
    reviews: {
      currentRevision: revision,
      reviews: [],
      currentReviews: [],
      approvals: 1,
      changeRequests: 0,
      comments: 0,
    },
    ciSuccess: true,
    statusChecks: [mergeStatusCheck()],
    directPushBlocked: true,
    rules: [branchRuleRecord()],
  };
}
