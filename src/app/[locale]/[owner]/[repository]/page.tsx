import { GitBranch, ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { BranchList } from "@/components/repositories/branch-list";
import { CIRunList } from "@/components/repositories/ci-run-list";
import { IssueList } from "@/components/repositories/issue-list";
import { MergeRequestList } from "@/components/repositories/merge-request-list";
import { RepositoryHeader } from "@/components/repositories/repository-header";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getBranches, getCIRuns, getOpenIssues, getOpenMergeRequests, getPublicRepository } from "@/lib/lorehub-api";

import styles from "./repository.module.css";

type RepositoryPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function RepositoryPage({ params }: RepositoryPageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, repository] = await Promise.all([getDictionary(locale), getPublicRepository(owner, slug)]);
  if (!repository.ok && repository.reason === "not-found") {
    notFound();
  }
  if (!repository.ok) {
    return (
      <div className={styles.unavailable}>
        <EmptyState
          icon={<ServerOff aria-hidden="true" />}
          title={dictionary.repository.unavailable}
          body={dictionary.home.apiUnavailableBody}
          tone="warning"
        />
      </div>
    );
  }

  const [branches, issues, mergeRequests, ciRuns] = await Promise.all([
    getBranches(owner, slug),
    getOpenIssues(owner, slug),
    getOpenMergeRequests(owner, slug),
    getCIRuns(owner, slug),
  ]);
  return (
    <>
      <RepositoryHeader repository={repository.data} locale={locale} dictionary={dictionary} />
      <div className={styles.content}>
        <section className={styles.panel} aria-labelledby="branches-title" id="branches">
          <div className={styles.panelHeading}>
            <div>
              <h2 id="branches-title">{dictionary.repository.branchesTitle}</h2>
              <p>{dictionary.repository.branchesDescription}</p>
            </div>
            <span className={styles.defaultBranch}>
              <GitBranch aria-hidden="true" size={15} />
              {dictionary.repository.defaultBranch}: <code>{repository.data.defaultBranch}</code>
            </span>
          </div>
          {branches.ok ? (
            <BranchList branches={branches.data} dictionary={dictionary} />
          ) : (
            <EmptyState
              icon={<ServerOff aria-hidden="true" />}
              title={dictionary.repository.unavailable}
              body={dictionary.repository.branchesDescription}
              tone="warning"
            />
          )}
        </section>

        <section className={styles.panel} aria-labelledby="issues-title" id="issues">
          <div className={styles.panelHeading}>
            <div>
              <h2 id="issues-title">{dictionary.repository.issuesTitle}</h2>
              <p>{dictionary.repository.issuesDescription}</p>
            </div>
          </div>
          {issues.ok ? (
            <IssueList issues={issues.data} dictionary={dictionary} locale={locale} />
          ) : (
            <EmptyState
              icon={<ServerOff aria-hidden="true" />}
              title={dictionary.repository.unavailable}
              body={dictionary.repository.issuesDescription}
              tone="warning"
            />
          )}
        </section>

        <section className={styles.panel} aria-labelledby="reviews-title" id="reviews">
          <div className={styles.panelHeading}>
            <div>
              <h2 id="reviews-title">{dictionary.repository.mergeRequestsTitle}</h2>
              <p>{dictionary.repository.mergeRequestsDescription}</p>
            </div>
          </div>
          {mergeRequests.ok ? (
            <MergeRequestList mergeRequests={mergeRequests.data} dictionary={dictionary} />
          ) : (
            <EmptyState
              icon={<ServerOff aria-hidden="true" />}
              title={dictionary.repository.unavailable}
              body={dictionary.repository.mergeRequestsDescription}
              tone="warning"
            />
          )}
        </section>

        <section className={styles.panel} aria-labelledby="actions-title" id="actions">
          <div className={styles.panelHeading}>
            <div>
              <h2 id="actions-title">{dictionary.repository.ciRunsTitle}</h2>
              <p>{dictionary.repository.ciRunsDescription}</p>
            </div>
          </div>
          {ciRuns.ok ? (
            <CIRunList runs={ciRuns.data} dictionary={dictionary} />
          ) : (
            <EmptyState
              icon={<ServerOff aria-hidden="true" />}
              title={dictionary.repository.unavailable}
              body={dictionary.repository.ciRunsDescription}
              tone="warning"
            />
          )}
        </section>
      </div>
    </>
  );
}
