"use client";

import { Clock3, Pencil, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, WikiPage, WikiRevision } from "@/lib/api-types";
import { deleteJsonWithBody, patchJson, type MutationFailureKind } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { repositoryPath } from "@/lib/routes";

import { FlashNotice } from "../ui/flash-notice";
import { MarkdownContent } from "./markdown-content";
import styles from "./wiki-document.module.css";

type WikiDocumentProps = {
  current: WikiPage;
  dictionary: Dictionary;
  history: WikiRevision[];
  locale: Locale;
  owner: string;
  repository: string;
  revision: WikiRevision | null;
  session: AuthSession;
};

export function WikiDocument(props: WikiDocumentProps) {
  const router = useRouter();
  const labels = props.dictionary.wikiPage;
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [title, setTitle] = useState(props.current.title);
  const [slug, setSlug] = useState(props.current.slug);
  const [body, setBody] = useState(props.current.body);
  const [summary, setSummary] = useState("");
  const basePath = repositoryPath(props.locale, props.owner, props.repository, "wiki");
  const pagePath = `${basePath}/${encodeURIComponent(props.current.slug)}`;
  const apiPath = wikiAPIPath(props.owner, props.repository, props.current.slug);
  const canWrite = props.current.viewerCanWrite && props.session.status === "authenticated" && props.revision === null;
  const displayed = props.revision ?? props.current;

  async function savePage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (props.session.status !== "authenticated") return;
    setSaving(true);
    setFailure(null);
    const result = await patchJson<WikiPage>(
      apiPath,
      {
        title: title.trim(),
        slug: slug.trim(),
        body,
        editSummary: summary.trim(),
        expectedVersion: props.current.version,
      },
      props.session.csrfToken,
    );
    setSaving(false);
    if (!result.ok) {
      setFailure(failureMessage(result.code, result.kind, props.dictionary));
      return;
    }
    const nextPath = `${basePath}/${encodeURIComponent(result.data.slug)}`;
    setEditing(false);
    router.push(nextPath);
    router.refresh();
  }

  async function deletePage() {
    if (props.session.status !== "authenticated" || !window.confirm(labels.deleteConfirm)) return;
    setSaving(true);
    setFailure(null);
    const result = await deleteJsonWithBody<null>(
      apiPath,
      { expectedVersion: props.current.version },
      props.session.csrfToken,
    );
    setSaving(false);
    if (!result.ok) {
      setFailure(failureMessage(result.code, result.kind, props.dictionary));
      return;
    }
    router.push(basePath);
    router.refresh();
  }

  return (
    <div className={styles.layout}>
      <section className={styles.document}>
        {props.revision && (
          <div className={styles.revisionNotice}>
            <span>
              {labels.revisionNotice
                .replace("{version}", String(props.revision.version))
                .replace("{current}", String(props.current.version))}
            </span>
            <Link href={pagePath}>{labels.viewCurrent}</Link>
          </div>
        )}
        <header className={styles.header}>
          <div>
            <h1>{displayed.title}</h1>
            <p>
              {formatMeta(labels.pageMeta, props.locale, displayed.version, editor(displayed), editedAt(displayed))}
            </p>
          </div>
          {canWrite && !editing && (
            <div className={styles.actions}>
              <button className={styles.secondaryButton} onClick={() => setEditing(true)} type="button">
                <Pencil aria-hidden="true" size={16} />
                {labels.edit}
              </button>
              <button className={styles.dangerButton} disabled={saving} onClick={deletePage} type="button">
                <Trash2 aria-hidden="true" size={16} />
                {labels.delete}
              </button>
            </div>
          )}
        </header>
        {failure && <FlashNotice body={failure} title={props.dictionary.forms.submitFailed} tone="error" />}
        {editing ? (
          <form className={styles.editor} onSubmit={savePage}>
            <label htmlFor="wiki-edit-title">{labels.titleLabel}</label>
            <input
              id="wiki-edit-title"
              maxLength={256}
              onChange={(event) => setTitle(event.target.value)}
              required
              value={title}
            />
            <label htmlFor="wiki-edit-slug">{labels.slugLabel}</label>
            <input
              id="wiki-edit-slug"
              maxLength={160}
              onChange={(event) => setSlug(event.target.value)}
              required
              value={slug}
            />
            <label htmlFor="wiki-edit-body">{labels.bodyLabel}</label>
            <textarea
              id="wiki-edit-body"
              maxLength={1_048_576}
              onChange={(event) => setBody(event.target.value)}
              value={body}
            />
            <label htmlFor="wiki-edit-summary">{labels.summaryLabel}</label>
            <input
              id="wiki-edit-summary"
              maxLength={256}
              onChange={(event) => setSummary(event.target.value)}
              placeholder={labels.summaryPlaceholder}
              value={summary}
            />
            <div className={styles.formActions}>
              <button className={styles.primaryButton} disabled={saving} type="submit">
                {saving ? labels.saving : labels.save}
              </button>
              <button className={styles.secondaryButton} onClick={() => setEditing(false)} type="button">
                {labels.cancel}
              </button>
            </div>
          </form>
        ) : (
          <div className={styles.body}>
            <MarkdownContent body={displayed.body ?? ""} />
          </div>
        )}
      </section>
      <aside className={styles.history}>
        <h2>
          <Clock3 aria-hidden="true" size={16} />
          {labels.history}
        </h2>
        <ol>
          {props.history.map((item) => {
            const selected = item.version === displayed.version;
            const href = item.version === props.current.version ? pagePath : `${pagePath}?version=${item.version}`;
            return (
              <li data-selected={selected} key={item.version}>
                <Link aria-current={selected ? "page" : undefined} href={href}>
                  <strong>
                    {item.version === props.current.version
                      ? labels.currentVersion
                      : labels.version.replace("{version}", String(item.version))}
                  </strong>
                  <span>{item.editSummary || labels.noSummary}</span>
                  <small>{formatHistoryMeta(props.locale, item.editedBy, item.createdAt)}</small>
                </Link>
              </li>
            );
          })}
        </ol>
      </aside>
    </div>
  );
}

function editor(value: WikiPage | WikiRevision): string {
  return "updatedBy" in value ? value.updatedBy : value.editedBy;
}

function editedAt(value: WikiPage | WikiRevision): string {
  return "updatedAt" in value ? value.updatedAt : value.createdAt;
}

function failureMessage(code: string | null, kind: MutationFailureKind, dictionary: Dictionary): string {
  if (code === "version_conflict") return dictionary.wikiPage.conflict;
  if (code === "invalid_input") return dictionary.wikiPage.invalid;
  return mutationFailureMessage(kind, dictionary);
}

function formatMeta(template: string, locale: Locale, version: number, author: string, date: string): string {
  return template
    .replace("{version}", String(version))
    .replace("{author}", author)
    .replace("{date}", formatDate(locale, date));
}

function formatHistoryMeta(locale: Locale, author: string, date: string): string {
  return `${author} · ${formatDate(locale, date)}`;
}

function formatDate(locale: Locale, date: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(date));
}

function wikiAPIPath(owner: string, repository: string, slug: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/wiki/${encodeURIComponent(
    slug,
  )}`;
}
