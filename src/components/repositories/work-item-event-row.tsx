"use client";

import { CircleCheck, CircleDot, Eye, GitMerge, GitPullRequest, Milestone, Pencil, Tag, User } from "lucide-react";
import Link from "next/link";
import type { CSSProperties, ReactNode } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import { labelTextColor, normalizeLabelColor, shortRevision } from "@/lib/format";
import { localizedPath } from "@/lib/routes";
import type { WorkItemEvent } from "@/lib/work-item-events";

import { RelativeTime } from "./issue-timeline-item";
import styles from "./work-item-event-row.module.css";

type WorkItemEventRowProps = {
  dictionary: Dictionary;
  event: WorkItemEvent;
  locale: Locale;
};

export function WorkItemEventRow({ dictionary, event, locale }: WorkItemEventRowProps) {
  const copy = dictionary.workItemEvents;
  const payload = event.payload;
  const values: Record<string, ReactNode> = {
    actor: (
      <Link className={styles.actor} href={localizedPath(locale, event.actor)}>
        {event.actor}
      </Link>
    ),
    time: <RelativeTime locale={locale} value={event.createdAt} />,
    assignee: <strong>{payload.assignee?.username ?? ""}</strong>,
    milestone: <strong>{payload.milestone?.title ?? ""}</strong>,
    reviewer: <strong>{payload.reviewer ?? ""}</strong>,
    revision: <code>{payload.revision ? shortRevision(payload.revision) : ""}</code>,
    label: payload.label ? <LabelChip color={payload.label.color} name={payload.label.name} /> : null,
  };
  return (
    <div className={styles.row} data-kind={event.kind}>
      <span aria-hidden="true" className={styles.gutter}>
        <EventIcon kind={event.kind} />
      </span>
      <p className={styles.text}>
        {fillTemplate(template(event, copy), values)}
        {event.kind === "retitled" && (
          <span className={styles.titleChange}>
            <del>{payload.oldTitle}</del> <strong>{payload.newTitle}</strong>
          </span>
        )}
      </p>
    </div>
  );
}

function template(event: WorkItemEvent, copy: Dictionary["workItemEvents"]): string {
  const templates: Record<WorkItemEvent["kind"], string> = {
    closed: copy.closed,
    reopened: copy.reopened,
    merged: event.payload.revision ? copy.mergedRevision : copy.merged,
    labeled: copy.labeled,
    unlabeled: copy.unlabeled,
    assigned: event.payload.assignee?.username === event.actor ? copy.selfAssigned : copy.assigned,
    unassigned: copy.unassigned,
    milestoned: copy.milestoned,
    demilestoned: copy.demilestoned,
    retitled: copy.retitled,
    review_requested: copy.reviewRequested,
    draft_ready: copy.draftReady,
  };
  return templates[event.kind];
}

function EventIcon({ kind }: { kind: WorkItemEvent["kind"] }) {
  switch (kind) {
    case "closed":
      return <CircleCheck size={14} />;
    case "reopened":
      return <CircleDot size={14} />;
    case "merged":
      return <GitMerge size={14} />;
    case "labeled":
    case "unlabeled":
      return <Tag size={14} />;
    case "assigned":
    case "unassigned":
      return <User size={14} />;
    case "milestoned":
    case "demilestoned":
      return <Milestone size={14} />;
    case "retitled":
      return <Pencil size={14} />;
    case "review_requested":
      return <Eye size={14} />;
    case "draft_ready":
      return <GitPullRequest size={14} />;
  }
}

function LabelChip({ color, name }: { color: string; name: string }) {
  const style = {
    "--label-color": normalizeLabelColor(color),
    "--label-fg": labelTextColor(color),
  } as CSSProperties;
  return (
    <span className={styles.label} style={style}>
      {name}
    </span>
  );
}

function fillTemplate(text: string, values: Record<string, ReactNode>): ReactNode[] {
  return text.split(/(\{[a-zA-Z]+\})/u).map((part, index) => {
    const name = part.startsWith("{") && part.endsWith("}") ? part.slice(1, -1) : null;
    const value = name ? values[name] : undefined;
    return <span key={index}>{value === undefined ? part : value}</span>;
  });
}
