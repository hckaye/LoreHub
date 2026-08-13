import { CheckCircle2, ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { CompareControls } from "@/components/repositories/compare-controls";
import { DiffView } from "@/components/repositories/diff-view";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary, type Dictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import type { Branch } from "@/lib/api-types";
import { compareOptions, loadComparison, type CompareStatus } from "@/lib/compare-revisions";
import { getBranches, getPublicRepository } from "@/lib/lorehub-api";

import styles from "@/components/repositories/compare-view.module.css";

type ComparePageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ source?: string; target?: string }>;
};

export const dynamic = "force-dynamic";

export default async function ComparePage({ params, searchParams }: ComparePageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const [dictionary, repository, branches] = await Promise.all([
    getDictionary(locale),
    getPublicRepository(owner, slug),
    getBranches(owner, slug),
  ]);
  if (!repository.ok && repository.reason === "not-found") notFound();
  if (!repository.ok) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const branchList: Branch[] = branches.ok ? branches.data : [];
  const base = query.source ?? repository.data.defaultBranch;
  const head = query.target ?? repository.data.defaultBranch;
  const { status, diff } = await loadComparison(owner, slug, { base, head }, branchList);
  const repositoryBase = `/${locale}/${encodeURIComponent(owner)}/${encodeURIComponent(slug)}`;
  return (
    <RepositorySection description={dictionary.commitHistory.compareDescription} title={dictionary.codeBrowser.compare}>
      <div className={styles.compare}>
        <CompareControls
          base={base}
          comparePath={`${repositoryBase}/compare`}
          dictionary={dictionary}
          head={head}
          options={compareOptions(branchList, [base, head])}
          pullRequestHref={`${repositoryBase}/pulls/new`}
        />
        <CompareStatusNote dictionary={dictionary} status={status} />
        {diff && !diff.ok && (
          <EmptyState
            body={dictionary.codeBrowser.unavailable}
            icon={<ServerOff />}
            title={dictionary.repository.unavailable}
          />
        )}
        {diff?.ok && <DiffView diff={diff.data} dictionary={dictionary} />}
      </div>
    </RepositorySection>
  );
}

function CompareStatusNote({ dictionary, status }: { dictionary: Dictionary; status: CompareStatus | null }) {
  const copy = dictionary.commitHistory;
  if (status === "identical") {
    return <p className={styles.status}>{copy.compareIdentical}</p>;
  }
  if (status === "mergeable") {
    return (
      <p className={styles.status} data-tone="success">
        <CheckCircle2 aria-hidden="true" size={16} />
        <strong>{copy.ableToMerge}</strong>
        {copy.ableToMergeHint}
      </p>
    );
  }
  return status === null ? <p className={styles.status}>{copy.compareChoose}</p> : null;
}
