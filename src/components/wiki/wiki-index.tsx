"use client";

import { BookOpenText, FileText, Plus, Search } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, WikiPage, WikiPageList } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { formatDate } from "@/lib/format";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { repositoryPath } from "@/lib/routes";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./wiki-index.module.css";

type WikiIndexProps = {
  data: WikiPageList;
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  query: string;
  repository: string;
  session: AuthSession;
};

export function WikiIndex(props: WikiIndexProps) {
  const router = useRouter();
  const [showForm, setShowForm] = useState(false);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [body, setBody] = useState("");
  const [searchValue, setSearchValue] = useState(props.query);
  const labels = props.dictionary.wikiPage;
  const basePath = repositoryPath(props.locale, props.owner, props.repository, "wiki");
  const apiPath = wikiAPIPath(props.owner, props.repository);
  const canWrite = props.data.viewerCanWrite && props.session.status === "authenticated";

  async function createPage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (props.session.status !== "authenticated" || !title.trim()) return;
    setSaving(true);
    setFailure(null);
    const result = await postJson<WikiPage>(
      apiPath,
      { title: title.trim(), slug: slug.trim(), body, editSummary: "" },
      props.session.csrfToken,
    );
    setSaving(false);
    if (!result.ok) {
      setFailure(
        result.code === "invalid_input" ? labels.invalid : mutationFailureMessage(result.kind, props.dictionary),
      );
      return;
    }
    router.push(`${basePath}/${encodeURIComponent(result.data.slug)}`);
    router.refresh();
  }

  function updateSearch(value: string) {
    setSearchValue(value);
    const nextQuery = value.trim() ? `?q=${encodeURIComponent(value)}` : "";
    router.replace(`${basePath}${nextQuery}`);
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1>{labels.title}</h1>
        {canWrite && (
          <button className={styles.primaryButton} onClick={() => setShowForm((current) => !current)} type="button">
            <Plus aria-hidden="true" size={16} />
            {labels.newPage}
          </button>
        )}
      </header>
      {showForm && (
        <form className={styles.editor} onSubmit={createPage}>
          <h2>{labels.createTitle}</h2>
          {failure && <FlashNotice body={failure} title={props.dictionary.forms.submitFailed} tone="error" />}
          <label htmlFor="wiki-title">{labels.titleLabel}</label>
          <input
            autoFocus
            id="wiki-title"
            maxLength={256}
            onChange={(event) => setTitle(event.target.value)}
            placeholder={labels.titlePlaceholder}
            required
            value={title}
          />
          <label htmlFor="wiki-slug">{labels.slugLabel}</label>
          <input id="wiki-slug" maxLength={160} onChange={(event) => setSlug(event.target.value)} value={slug} />
          <small>{labels.slugHelp}</small>
          <label htmlFor="wiki-body">{labels.bodyLabel}</label>
          <textarea
            id="wiki-body"
            maxLength={1_048_576}
            onChange={(event) => setBody(event.target.value)}
            placeholder={labels.bodyPlaceholder}
            value={body}
          />
          <div className={styles.formActions}>
            <button className={styles.primaryButton} disabled={saving} type="submit">
              {saving ? labels.creating : labels.create}
            </button>
            <button className={styles.secondaryButton} onClick={() => setShowForm(false)} type="button">
              {labels.cancel}
            </button>
          </div>
        </form>
      )}
      <div className={styles.layout}>
        <div className={styles.content}>
          {props.data.pages.length === 0 ? (
            <div className={styles.blankSlate}>
              <BookOpenText aria-hidden="true" size={32} />
              <h2>
                {props.query ? labels.noMatchesTitle : labels.welcomeTitle.replace("{repository}", props.repository)}
              </h2>
              <p>{props.query ? labels.noMatchesBody : labels.welcomeBody}</p>
              {!props.query && canWrite && (
                <button className={styles.primaryButton} onClick={() => setShowForm(true)} type="button">
                  <Plus aria-hidden="true" size={16} />
                  {labels.createFirstPage}
                </button>
              )}
            </div>
          ) : (
            <section className={styles.contentList}>
              <h2>{props.data.pages[0]?.title ?? labels.title}</h2>
              <div className={styles.list}>
                {props.data.pages.map((page) => (
                  <Link className={styles.item} href={`${basePath}/${encodeURIComponent(page.slug)}`} key={page.id}>
                    <FileText aria-hidden="true" size={20} />
                    <span>
                      <strong>{page.title}</strong>
                      <small>
                        {formatMeta(labels.pageMeta, props.locale, page.version, page.updatedBy, page.updatedAt)}
                      </small>
                    </span>
                  </Link>
                ))}
              </div>
            </section>
          )}
        </div>
        <aside className={styles.sidebar}>
          <section className={styles.pagesBox}>
            <h2>{labels.pagesTitle}</h2>
            <form action={basePath} className={styles.search} method="get" role="search">
              <label className={styles.srOnly} htmlFor="wiki-search">
                {labels.searchLabel}
              </label>
              <Search aria-hidden="true" size={16} />
              <input
                id="wiki-search"
                maxLength={200}
                name="q"
                onChange={(event) => updateSearch(event.target.value)}
                placeholder={labels.searchPlaceholder}
                type="search"
                value={searchValue}
              />
            </form>
            <nav aria-label={labels.pagesTitle} className={styles.pageLinks}>
              {props.data.pages.map((page) => (
                <Link href={`${basePath}/${encodeURIComponent(page.slug)}`} key={page.id}>
                  <FileText aria-hidden="true" size={15} />
                  <span>{page.title}</span>
                </Link>
              ))}
              {props.data.pages.length === 0 && <p>{props.query ? labels.noMatchesBody : labels.emptyBody}</p>}
            </nav>
          </section>
        </aside>
      </div>
    </div>
  );
}

function formatMeta(template: string, locale: Locale, version: number, author: string, date: string): string {
  return template
    .replace("{version}", String(version))
    .replace("{author}", author)
    .replace("{date}", formatDate(date, locale));
}

function wikiAPIPath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/wiki`;
}
