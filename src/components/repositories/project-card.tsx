"use client";

import { FileText, GitPullRequest, MoreHorizontal } from "lucide-react";
import Link from "next/link";
import { FormEvent, useState } from "react";

import { PopupMenu } from "@/components/ui/popup-menu";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { ProjectColumn, ProjectItem } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

import styles from "./project-board.module.css";

type ProjectCardProps = {
  busy: boolean;
  canWrite: boolean;
  columns: ProjectColumn[];
  dictionary: Dictionary;
  item: ProjectItem;
  locale: Locale;
  onDelete(itemID: string): Promise<void>;
  onUpdate(itemID: string, input: Record<string, string>): Promise<boolean>;
  owner: string;
  repository: string;
};

export function ProjectCard(props: ProjectCardProps) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(props.item.title);
  const [body, setBody] = useState(props.item.body);
  const labels = props.dictionary.projectsPage.board;
  const content = cardContent(props);

  async function saveDraft(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onUpdate(props.item.id, { title: title.trim(), body: body.trim() })) {
      setEditing(false);
    }
  }

  return (
    <article className={styles.card}>
      <div className={styles.cardHeading}>
        <span className={styles.cardKind} data-kind={props.item.kind}>
          {props.item.kind === "merge_request" ? (
            <GitPullRequest aria-hidden="true" size={16} />
          ) : (
            <FileText aria-hidden="true" size={16} />
          )}
          {props.item.kind === "issue"
            ? labels.issue
            : props.item.kind === "merge_request"
              ? labels.pullRequest
              : labels.draft}
        </span>
        {props.canWrite && (
          <PopupMenu
            className={styles.columnMenu}
            panelClassName={styles.menu}
            trigger={<MoreHorizontal aria-hidden="true" size={16} />}
            triggerClassName={styles.iconButton}
            triggerProps={{ "aria-label": labels.deleteCard }}
          >
            {(close) => (
              <>
                {props.item.kind === "draft" && (
                  <button
                    onClick={() => {
                      close();
                      setEditing(true);
                    }}
                    role="menuitem"
                    type="button"
                  >
                    {labels.editDraft}
                  </button>
                )}
                <button
                  className={styles.dangerAction}
                  onClick={() => {
                    close();
                    props.onDelete(props.item.id);
                  }}
                  role="menuitem"
                  type="button"
                >
                  {labels.deleteCard}
                </button>
              </>
            )}
          </PopupMenu>
        )}
      </div>
      {content.href ? (
        <Link className={styles.cardTitle} href={content.href}>
          {props.item.title}
        </Link>
      ) : (
        <strong className={styles.cardTitle}>{props.item.title}</strong>
      )}
      {props.item.body && !editing && <p className={styles.cardBody}>{props.item.body}</p>}
      <small>{content.meta}</small>
      {editing && (
        <form className={styles.inlineForm} onSubmit={saveDraft}>
          <label htmlFor={`draft-title-${props.item.id}`}>{labels.draftTitle}</label>
          <input
            id={`draft-title-${props.item.id}`}
            maxLength={512}
            onChange={(event) => setTitle(event.target.value)}
            required
            value={title}
          />
          <label htmlFor={`draft-body-${props.item.id}`}>{labels.draftBody}</label>
          <textarea
            id={`draft-body-${props.item.id}`}
            maxLength={65_536}
            onChange={(event) => setBody(event.target.value)}
            value={body}
          />
          <div className={styles.formActions}>
            <button className={styles.primaryButton} disabled={props.busy} type="submit">
              {labels.saveDraft}
            </button>
            <button className={styles.iconButton} onClick={() => setEditing(false)} type="button">
              {props.dictionary.common.cancel}
            </button>
          </div>
        </form>
      )}
      {props.canWrite && props.columns.length > 1 && (
        <label className={styles.moveControl}>
          <span>{labels.moveTo}</span>
          <select
            disabled={props.busy}
            onChange={(event) => props.onUpdate(props.item.id, { columnId: event.target.value })}
            value={props.item.columnId}
          >
            {props.columns.map((column) => (
              <option key={column.id} value={column.id}>
                {column.name}
              </option>
            ))}
          </select>
        </label>
      )}
    </article>
  );
}

function cardContent(props: ProjectCardProps): { href: string | null; meta: string } {
  const labels = props.dictionary.projectsPage.board;
  if (props.item.kind === "draft" || props.item.number === null) {
    return { href: null, meta: labels.draftBy.replace("{author}", props.item.author) };
  }
  const section = props.item.kind === "issue" ? "issues" : "pulls";
  const template = props.item.state === "merged" ? labels.mergedBy : labels.openedBy;
  return {
    href: `${repositoryPath(props.locale, props.owner, props.repository, section)}/${props.item.number}`,
    meta: template.replace("{number}", String(props.item.number)).replace("{author}", props.item.author),
  };
}
