"use client";

import { Plus, Tags } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Branch, Release, ReleasePage } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import { ReleaseCard } from "./release-card";
import { ReleaseCreateForm } from "./release-create-form";
import styles from "./release-list.module.css";

type ReleaseListProps = {
  branches: Branch[];
  data: ReleasePage;
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
  session: AuthSession;
};

export function ReleaseList(props: ReleaseListProps) {
  const [releases, setReleases] = useState(props.data.releases);
  const [showForm, setShowForm] = useState(false);
  const labels = props.dictionary.releasesPage;
  const canWrite = props.data.viewerCanWrite && props.session.status === "authenticated";
  const searchParams = useSearchParams();
  const basePath = repositoryPath(props.locale, props.owner, props.repository, "releases");

  function replaceRelease(updated: Release) {
    setReleases((current) => current.map((release) => (release.id === updated.id ? updated : release)));
  }

  function prependRelease(created: Release) {
    setReleases((current) => [created, ...current]);
    setShowForm(false);
  }

  function pageHref(page: number): string {
    const params = new URLSearchParams(searchParams?.toString() ?? "");
    params.set("page", String(page));
    return `${basePath}?${params.toString()}`;
  }

  const latestId = props.data.page === 1 ? releases[0]?.id : undefined;

  return (
    <div className={styles.page}>
      {canWrite && (
        <div className={styles.createArea}>
          <button className={styles.primaryButton} onClick={() => setShowForm((visible) => !visible)} type="button">
            <Plus aria-hidden="true" size={16} />
            {labels.newRelease}
          </button>
          {showForm && (
            <ReleaseCreateForm
              branches={props.branches}
              dictionary={props.dictionary}
              onCancel={() => setShowForm(false)}
              onCreated={prependRelease}
              owner={props.owner}
              repository={props.repository}
              session={props.session}
            />
          )}
        </div>
      )}
      {releases.length === 0 ? (
        <EmptyState body={labels.emptyBody} icon={<Tags aria-hidden="true" />} title={labels.emptyTitle} />
      ) : (
        <div className={styles.list}>
          {releases.map((release) => (
            <ReleaseCard
              dictionary={props.dictionary}
              isLatest={release.id === latestId}
              key={release.id}
              locale={props.locale}
              onChange={replaceRelease}
              onDelete={(releaseID) => setReleases((current) => current.filter((item) => item.id !== releaseID))}
              owner={props.owner}
              release={release}
              repository={props.repository}
              session={props.session}
            />
          ))}
        </div>
      )}
      {(props.data.page > 1 || props.data.hasNext) && (
        <nav aria-label={labels.pagination} className={styles.pagination}>
          {props.data.page > 1 && <Link href={pageHref(props.data.page - 1)}>{labels.previous}</Link>}
          <span>{labels.page.replace("{page}", String(props.data.page))}</span>
          {props.data.hasNext && <Link href={pageHref(props.data.page + 1)}>{labels.next}</Link>}
        </nav>
      )}
    </div>
  );
}
