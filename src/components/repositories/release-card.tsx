"use client";

import { GitBranch, GitCommitHorizontal, Pencil, Rocket, Tag, Trash2 } from "lucide-react";
import Link from "next/link";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Release } from "@/lib/api-types";
import { formatRelativeTime, formatTimestamp, shortRevision } from "@/lib/format";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { deleteRelease, publishRelease, updateRelease } from "@/lib/release-client";
import { repositoryPath } from "@/lib/routes";

import { FlashNotice } from "../ui/flash-notice";
import { UserAvatar } from "../ui/user-avatar";
import { MarkdownContent } from "../wiki/markdown-content";
import { ReleaseAssets } from "./release-assets";
import styles from "./release-list.module.css";

type ReleaseCardProps = {
  dictionary: Dictionary;
  isLatest?: boolean;
  locale: Locale;
  owner: string;
  repository: string;
  release: Release;
  session: AuthSession;
  onChange(release: Release): void;
  onDelete(releaseID: string): void;
};

export function ReleaseCard(props: ReleaseCardProps) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(props.release.title);
  const [notes, setNotes] = useState(props.release.notes);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const labels = props.dictionary.releasesPage;
  const authenticated = props.session.status === "authenticated";
  const anchorId = `release-${props.release.tagName}`;

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (props.session.status !== "authenticated") return;
    setBusy(true);
    setFailure("");
    const result = await updateRelease(
      props.owner,
      props.repository,
      props.release.id,
      { title: title.trim(), notes: notes.trim(), expectedVersion: props.release.version },
      props.session.csrfToken,
    );
    setBusy(false);
    if (!result.ok) return fail(result.kind);
    setEditing(false);
    props.onChange(result.data);
  }

  async function publish() {
    if (props.session.status !== "authenticated") return;
    setBusy(true);
    setFailure("");
    const result = await publishRelease(
      props.owner,
      props.repository,
      props.release.id,
      props.release.version,
      props.session.csrfToken,
    );
    setBusy(false);
    if (!result.ok) return fail(result.kind);
    props.onChange(result.data);
  }

  async function remove() {
    if (props.session.status !== "authenticated" || !window.confirm(labels.confirmDeleteRelease)) return;
    setBusy(true);
    setFailure("");
    const result = await deleteRelease(
      props.owner,
      props.repository,
      props.release.id,
      props.release.version,
      props.session.csrfToken,
    );
    setBusy(false);
    if (!result.ok) return fail(result.kind);
    props.onDelete(props.release.id);
  }

  function fail(kind: "unauthorized" | "forbidden" | "invalid" | "conflict" | "unavailable") {
    setFailure(mutationFailureMessage(kind, props.dictionary));
  }

  const repositoryHref = repositoryPath(props.locale, props.owner, props.repository);
  const revisionQuery = encodeURIComponent(props.release.revision);
  const revisionHref = `${repositoryHref}/commit?revision=${revisionQuery}`;
  const releasedAt = props.release.publishedAt ?? props.release.createdAt;
  return (
    <article className={styles.releaseRow} id={anchorId}>
      <div className={styles.rail}>
        <span className={styles.railTime} title={formatTimestamp(releasedAt, props.locale)}>
          {formatRelativeTime(releasedAt, props.locale)}
        </span>
        <span className={styles.railAuthor}>
          <UserAvatar name={props.release.createdBy} size={20} />
          <strong>{props.release.createdBy}</strong>
        </span>
        <span className={styles.railTag}>
          <Tag aria-hidden="true" size={16} />
          <a href={`#${anchorId}`}>{props.release.tagName}</a>
        </span>
        <span className={styles.railBranch}>
          <GitBranch aria-hidden="true" size={16} />
          {props.release.sourceBranch}
        </span>
        <Link className={styles.railRevision} href={revisionHref} title={props.release.revision}>
          <GitCommitHorizontal aria-hidden="true" size={16} />
          {shortRevision(props.release.revision)}
        </Link>
      </div>
      <div className={styles.releaseCard}>
        <header className={styles.releaseHeader}>
          <div className={styles.titleLine}>
            <h2 className={styles.releaseTitle}>
              <a href={`#${anchorId}`}>{props.release.title}</a>
            </h2>
            {props.isLatest && <span className={styles.latest}>{labels.latest}</span>}
            <span className={props.release.state === "published" ? styles.published : styles.draft}>
              {props.release.state === "published" ? labels.published : labels.draft}
            </span>
          </div>
          {props.release.viewerCanWrite && authenticated && (
            <div className={styles.cardActions}>
              {props.release.state === "draft" && (
                <button disabled={busy} onClick={publish} type="button">
                  <Rocket aria-hidden="true" size={15} />
                  {labels.publish}
                </button>
              )}
              <button disabled={busy} onClick={() => setEditing((value) => !value)} type="button">
                <Pencil aria-hidden="true" size={15} />
                {labels.edit}
              </button>
              <button className={styles.dangerButton} disabled={busy} onClick={remove} type="button">
                <Trash2 aria-hidden="true" size={15} />
                {labels.deleteRelease}
              </button>
            </div>
          )}
        </header>
        {failure && <FlashNotice body={failure} title={props.dictionary.forms.submitFailed} tone="error" />}
        {editing ? (
          <form className={styles.editForm} onSubmit={save}>
            <label>
              <span>{labels.titleLabel}</span>
              <input maxLength={512} onChange={(event) => setTitle(event.target.value)} required value={title} />
            </label>
            <label>
              <span>{labels.notesLabel}</span>
              <textarea maxLength={1_048_576} onChange={(event) => setNotes(event.target.value)} value={notes} />
            </label>
            <div className={styles.formActions}>
              <button className={styles.primaryButton} disabled={busy} type="submit">
                {busy ? labels.saving : labels.save}
              </button>
              <button className={styles.secondaryButton} onClick={() => setEditing(false)} type="button">
                {props.dictionary.common.cancel}
              </button>
            </div>
          </form>
        ) : (
          props.release.notes && (
            <div className={styles.notes}>
              <MarkdownContent body={props.release.notes} />
            </div>
          )
        )}
        <ReleaseAssets
          dictionary={props.dictionary}
          onChange={props.onChange}
          onFailure={setFailure}
          owner={props.owner}
          release={props.release}
          repository={props.repository}
          session={props.session}
        />
      </div>
    </article>
  );
}
