import { LockKeyhole, ServerOff, Tag } from "lucide-react";
import Link from "next/link";

import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getRepositoryTags } from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

import styles from "./tags-page.module.css";

type TagsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function TagsPage({ params }: TagsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const dictionary = await getDictionary(locale);
  const labels = dictionary.tagsPage;
  const tags = await getRepositoryTags(owner, repository);

  if (!tags.ok) {
    return (
      <EmptyState
        body={
          tags.reason === "forbidden" || tags.reason === "unauthorized" ? labels.forbiddenBody : labels.unavailableBody
        }
        icon={
          tags.reason === "forbidden" || tags.reason === "unauthorized" ? (
            <LockKeyhole aria-hidden="true" />
          ) : (
            <ServerOff aria-hidden="true" />
          )
        }
        title={
          tags.reason === "forbidden" || tags.reason === "unauthorized"
            ? labels.forbiddenTitle
            : labels.unavailableTitle
        }
        tone="warning"
      />
    );
  }

  return (
    <RepositorySection description={labels.description} title={labels.title}>
      <RepositoryPanel description={labels.description} title={labels.title}>
        <p className={styles.note}>{labels.sourceNote}</p>
        {tags.data.length === 0 ? (
          <EmptyState body={labels.emptyBody} icon={<Tag aria-hidden="true" />} title={labels.emptyTitle} />
        ) : (
          <div className={styles.tableWrap}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th scope="col">{labels.name}</th>
                  <th scope="col">{labels.revision}</th>
                  <th scope="col">{labels.createdAt}</th>
                  <th scope="col">{labels.createdBy}</th>
                </tr>
              </thead>
              <tbody>
                {tags.data.map((tag) => (
                  <tr key={`${tag.name}-${tag.revision}`}>
                    <td>
                      <Link
                        href={`${repositoryPath(locale, owner, repository)}?revision=${encodeURIComponent(
                          tag.revision,
                        )}`}
                      >
                        <Tag aria-hidden="true" size={15} />
                        {tag.name}
                      </Link>
                    </td>
                    <td>
                      <code title={tag.revision}>{shortRevision(tag.revision)}</code>
                    </td>
                    <td>
                      <time dateTime={tag.createdAt}>{formatDate(tag.createdAt, locale)}</time>
                    </td>
                    <td>{tag.createdBy}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </RepositoryPanel>
    </RepositorySection>
  );
}

function shortRevision(revision: string): string {
  return revision.length > 12 ? revision.slice(0, 12) : revision;
}

function formatDate(value: string, locale: "en" | "ja"): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
}
