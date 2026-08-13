import { z } from "zod";

import type { CommentPage } from "./comment-page-types";

export const workItemEventKinds = [
  "closed",
  "reopened",
  "labeled",
  "unlabeled",
  "assigned",
  "unassigned",
  "milestoned",
  "demilestoned",
  "retitled",
  "merged",
  "review_requested",
  "draft_ready",
] as const;

export type WorkItemEventKind = (typeof workItemEventKinds)[number];

export type WorkItemEvent = {
  id: string;
  itemKind: "issue" | "merge_request";
  itemId: string;
  actor: string;
  kind: WorkItemEventKind;
  payload: WorkItemEventPayload;
  createdAt: string;
};

export type WorkItemEventPayload = {
  label?: { id: string; name: string; color: string };
  assignee?: { id: string; username: string; displayName: string; avatarUrl: string };
  milestone?: { id: string; number: number; title: string };
  oldTitle?: string;
  newTitle?: string;
  reviewer?: string;
  revision?: string;
};

const timestamp = z.string().refine((value) => Number.isFinite(Date.parse(value)));

const payloadSchema = z.object({
  label: z.object({ id: z.string(), name: z.string(), color: z.string() }).loose().optional(),
  assignee: z
    .object({ id: z.string(), username: z.string(), displayName: z.string(), avatarUrl: z.string() })
    .loose()
    .optional(),
  milestone: z.object({ id: z.string(), number: z.number(), title: z.string() }).loose().optional(),
  oldTitle: z.string().optional(),
  newTitle: z.string().optional(),
  reviewer: z.string().optional(),
  revision: z.string().optional(),
});

const eventSchema = z.object({
  id: z.string().min(1),
  itemKind: z.enum(["issue", "merge_request"]),
  itemId: z.string().min(1),
  actor: z.string().min(1),
  kind: z.enum(workItemEventKinds),
  payload: payloadSchema,
  createdAt: timestamp,
});

const eventPageSchema = z.object({
  items: z.array(eventSchema),
  nextCursor: z.string().optional(),
  hasMore: z.boolean(),
});

export const workItemEventPageSize = 100;

export function workItemEventsPath(owner: string, repository: string, resource: string, number: number): string {
  const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`;
  return `${base}/${resource}/${number}/events?limit=${workItemEventPageSize}`;
}

export function parseWorkItemEvents(value: unknown): WorkItemEvent[] | null {
  const result = eventPageSchema.safeParse(value);
  return result.success ? result.data.items : null;
}

export type TimelineEntry<TComment> =
  | { kind: "comment"; id: string; createdAt: string; comment: TComment }
  | { kind: "event"; id: string; createdAt: string; event: WorkItemEvent };

// Events cover the whole item while comments are paginated, so each page shows the
// events that fall inside its comment window. The first page also shows everything
// before its first comment and the last page everything after its last comment.
export function mergeConversationTimeline<TComment extends { id: string; createdAt: string }>(
  comments: CommentPage<TComment> | null,
  events: WorkItemEvent[],
): Array<TimelineEntry<TComment>> {
  const entries: Array<TimelineEntry<TComment>> = (comments?.items ?? []).map((comment) => ({
    kind: "comment",
    id: comment.id,
    createdAt: comment.createdAt,
    comment,
  }));
  for (const event of visibleEvents(comments, events)) {
    entries.push({ kind: "event", id: event.id, createdAt: event.createdAt, event });
  }
  return entries.sort(byCreatedAt);
}

function visibleEvents<TComment extends { createdAt: string }>(
  comments: CommentPage<TComment> | null,
  events: WorkItemEvent[],
): WorkItemEvent[] {
  const items = comments?.items ?? [];
  if (!comments || items.length === 0) return events;
  const first = Date.parse(items[0]!.createdAt);
  const last = Date.parse(items[items.length - 1]!.createdAt);
  return events.filter((event) => {
    const time = Date.parse(event.createdAt);
    if (comments.page > 1 && time < first) return false;
    if (comments.hasNext && time > last) return false;
    return true;
  });
}

function byCreatedAt<TComment>(left: TimelineEntry<TComment>, right: TimelineEntry<TComment>): number {
  const difference = Date.parse(left.createdAt) - Date.parse(right.createdAt);
  if (difference !== 0) return difference;
  if (left.kind === right.kind) return 0;
  return left.kind === "comment" ? -1 : 1;
}
