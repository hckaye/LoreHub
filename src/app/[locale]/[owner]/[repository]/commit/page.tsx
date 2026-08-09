import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { DiffView } from "@/components/repositories/diff-view";
import { RepositoryPanel } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getLoreDiff, getPublicRepository, getRevision } from "@/lib/lorehub-api";

import styles from "@/components/repositories/code-detail.module.css";

type RevisionPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ revision?: string }>;
};

export const dynamic = "force-dynamic";

export default async function RevisionPage({ params, searchParams }: RevisionPageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const [dictionary, repository] = await Promise.all([getDictionary(locale), getPublicRepository(owner, slug)]);
  if (!repository.ok && repository.reason === "not-found") notFound();
  if (!repository.ok || !query.revision) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const revision = await getRevision(owner, slug, query.revision);
  if (!revision.ok) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const diff = revision.data.parents[0]
    ? await getLoreDiff(owner, slug, revision.data.parents[0], revision.data.revision)
    : null;
  return (
    <RepositoryPanel
      description={revision.data.message ?? dictionary.codeBrowser.revision}
      title={revision.data.revision}
    >
      <div className={styles.panel}>
        <div className={styles.status}>
          <p>{revision.data.message || dictionary.codeBrowser.revision}</p>
          <p className={styles.meta}>
            {dictionary.pullRequestDetail.commits}: {revision.data.number}
          </p>
        </div>
        {diff?.ok && <DiffView diff={diff.data} dictionary={dictionary} />}
      </div>
    </RepositoryPanel>
  );
}
