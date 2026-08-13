import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { workItemEvents } from "../src/i18n/dictionaries/work-item-events";
import type { CommentPage } from "../src/lib/comment-page-types";
import { mergeConversationTimeline, parseWorkItemEvents, workItemEventsPath } from "../src/lib/work-item-events";

const issuePagePath = "src/app/[locale]/[owner]/[repository]/issues/[number]/page.tsx";
const pullPagePath = "src/app/[locale]/[owner]/[repository]/pulls/[number]/page.tsx";
const issueConversationPath = "src/components/repositories/issue-conversation.tsx";
const pullConversationPath = "src/components/repositories/pull-request-conversation.tsx";
const eventRowPath = "src/components/repositories/work-item-event-row.tsx";
const serverAPIPath = "src/lib/lorehub-api.ts";

test("work item event parser accepts the API contract and rejects unknown kinds", () => {
  const parsed = parseWorkItemEvents(eventPage());
  assert.ok(parsed);
  assert.equal(parsed.length, 2);
  assert.equal(parsed[0]?.kind, "labeled");
  assert.equal(parsed[0]?.payload.label?.name, "bug");
  assert.equal(parsed[1]?.payload.oldTitle, "Old title");

  const unknownKind = eventPage();
  unknownKind.items[0]!.kind = "pinned";
  assert.equal(parseWorkItemEvents(unknownKind), null);

  const missingActor = eventPage();
  missingActor.items[0]!.actor = "";
  assert.equal(parseWorkItemEvents(missingActor), null);
  assert.equal(parseWorkItemEvents({ items: [] }), null);
});

test("event requests target the chronological timeline endpoints", () => {
  assert.equal(
    workItemEventsPath("acme team", "game/client", "issues", 7),
    "/api/v1/repositories/acme%20team/game%2Fclient/issues/7/events?limit=100",
  );
  assert.equal(
    workItemEventsPath("acme", "game", "merge-requests", 3),
    "/api/v1/repositories/acme/game/merge-requests/3/events?limit=100",
  );
});

test("timeline merges events with the comments of the shown page", () => {
  const events = parseWorkItemEvents(eventPage());
  assert.ok(events);
  const single = mergeConversationTimeline(commentPage({ page: 1, hasNext: false }), events);
  assert.deepEqual(
    single.map((entry) => entry.kind),
    ["event", "comment", "event", "comment"],
  );

  const firstOfTwo = mergeConversationTimeline(commentPage({ page: 1, hasNext: true }), events);
  assert.deepEqual(
    firstOfTwo.map((entry) => entry.id),
    ["event-1", "comment-1", "event-2", "comment-2"],
  );

  const laterPage = mergeConversationTimeline(
    { items: [comment("comment-9", "2026-08-13T05:00:00Z")], totalCount: 9, page: 2, perPage: 5, hasNext: false },
    events,
  );
  assert.deepEqual(
    laterPage.map((entry) => entry.id),
    ["comment-9"],
  );
  assert.equal(mergeConversationTimeline(null, events).length, events.length);
});

test("conversations render event rows next to unchanged comment cards", async () => {
  const [issuePage, pullPage, issueConversation, pullConversation, eventRow, serverAPI] = await Promise.all([
    readFile(issuePagePath, "utf8"),
    readFile(pullPagePath, "utf8"),
    readFile(issueConversationPath, "utf8"),
    readFile(pullConversationPath, "utf8"),
    readFile(eventRowPath, "utf8"),
    readFile(serverAPIPath, "utf8"),
  ]);
  assert.match(serverAPI, /getIssueEvents/u);
  assert.match(serverAPI, /getMergeRequestEvents/u);
  assert.match(serverAPI, /parseWorkItemEvents\(result\.data\)/u);
  assert.match(issuePage, /getIssueEvents\(owner, repository, number\)/u);
  assert.match(pullPage, /getMergeRequestEvents\(owner, slug, number\)/u);
  for (const conversation of [issueConversation, pullConversation]) {
    assert.match(conversation, /mergeConversationTimeline\(props\.comments, props\.events\)/u);
    assert.match(conversation, /<WorkItemEventRow/u);
  }
  assert.match(issueConversation, /<CommentCard/u);
  assert.match(pullConversation, /<Comment\b/u);
  assert.match(eventRow, /RelativeTime/u);
  assert.match(eventRow, /normalizeLabelColor/u);
  assert.match(eventRow, /data-kind=\{event\.kind\}/u);
});

test("event copy covers every recorded event kind in both locales", () => {
  for (const locale of [workItemEvents.en, workItemEvents.ja]) {
    for (const key of Object.keys(workItemEvents.en)) {
      const value = locale[key as keyof typeof workItemEvents.en];
      assert.ok(value.length > 0, `${key} must have copy`);
    }
    assert.match(locale.labeled, /\{label\}/u);
    assert.match(locale.assigned, /\{assignee\}/u);
    assert.match(locale.milestoned, /\{milestone\}/u);
    assert.match(locale.reviewRequested, /\{reviewer\}/u);
  }
});

function eventPage() {
  return {
    items: [
      {
        id: "event-1",
        itemKind: "issue",
        itemId: "issue-1",
        actor: "alice",
        kind: "labeled",
        payload: { label: { id: "label-1", name: "bug", color: "d73a4a" } },
        createdAt: "2026-08-13T00:30:00Z",
      },
      {
        id: "event-2",
        itemKind: "issue",
        itemId: "issue-1",
        actor: "bob",
        kind: "retitled",
        payload: { oldTitle: "Old title", newTitle: "New title" },
        createdAt: "2026-08-13T02:30:00Z",
      },
    ],
    hasMore: false,
  };
}

function commentPage(options: { page: number; hasNext: boolean }): CommentPage<ReturnType<typeof comment>> {
  return {
    items: [comment("comment-1", "2026-08-13T01:00:00Z"), comment("comment-2", "2026-08-13T03:00:00Z")],
    totalCount: options.hasNext ? 9 : 2,
    page: options.page,
    perPage: 2,
    hasNext: options.hasNext,
  };
}

function comment(id: string, createdAt: string) {
  return { id, createdAt };
}
