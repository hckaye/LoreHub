"use client";

import { Pencil, Rocket, Trash2 } from "lucide-react";
import Link from "next/link";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Release } from "@/lib/api-types";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { deleteRelease, publishRelease, updateRelease } from "@/lib/release-client";
import { repositoryPath } from "@/lib/routes";

import { FlashNotice } from "../ui/flash-notice";
import { ReleaseAssets } from "./release-assets";
import styles from "./release-list.module.css";

type ReleaseCardProps = {
  dictionary: Dictionary;
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
  return (
    <article className={styles.releaseCard}>
      <header className={styles.releaseHeader}>
        <div>
          <div className={styles.tagLine}>
            <span className={styles.tag}>{props.release.tagName}</span>
            <span className={props.release.state === "published" ? styles.published : styles.draft}>
              {props.release.state === "published" ? labels.published : labels.draft}
            </span>
          </div>
          <h2>{props.release.title}</h2>
          <p className={styles.meta}>
            {labels.createdBy.replace("{author}", props.release.createdBy)} ·{" "}
            {formatDate(props.release.createdAt, props.locale)}
          </p>
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
          <input maxLength={512} onChange={(event) => setTitle(event.target.value)} required value={title} />
          <textarea maxLength={1_048_576} onChange={(event) => setNotes(event.target.value)} value={notes} />
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
        props.release.notes && <p className={styles.notes}>{props.release.notes}</p>
      )}
      <div className={styles.revision}>
        <span>
          {labels.sourceBranch}: {props.release.sourceBranch}
        </span>
        <Link href={revisionHref} title={props.release.revision}>
          {labels.pinnedRevision}: <code>{props.release.revision.slice(0, 12)}</code>
        </Link>
      </div>
      <ReleaseAssets
        dictionary={props.dictionary}
        onChange={props.onChange}
        onFailure={setFailure}
        owner={props.owner}
        release={props.release}
        repository={props.repository}
        session={props.session}
      />
    </article>
  );
}

function formatDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(locale === "ja" ? "ja-JP" : "en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
