import { CircleAlert, ServerOff } from "lucide-react";
import Link from "next/link";

import { IssueList } from "@/components/repositories/issue-list";
import { RepositorySection } from "@/components/repositories/repository-section";
import { WorkItemListPagination } from "@/components/repositories/work-item-list-pagination";
import { WorkItemListToolbar } from "@/components/repositories/work-item-list-toolbar";
import { EmptyState } from "@/components/ui/empty-state";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getIssues, getPublicRepository } from "@/lib/lorehub-api";
import { type RawRepositoryWorkItemSearchParams, parseRepositoryIssueQuery } from "@/lib/repository-work-item-query";
import { repositoryMilestonesPath, repositoryPath } from "@/lib/routes";

import styles from "@/components/repositories/repository-section.module.css";

type IssuesPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<RawRepositoryWorkItemSearchParams & { created?: string }>;
};

export const dynamic = "force-dynamic";

export default async function IssuesPage({ params, searchParams }: IssuesPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const filters = parseRepositoryIssueQuery(query);
  const [dictionary, issues, repositoryResult] = await Promise.all([
    getDictionary(locale),
    getIssues(owner, repository, filters),
    getPublicRepository(owner, repository),
  ]);
  const archived = repositoryResult.ok && repositoryResult.data.archivedAt !== null;
  const basePath = repositoryPath(locale, owner, repository, "issues");
  return (
    <RepositorySection
      actions={
        <div className={styles.panelActions}>
          <Link className={styles.secondaryButton} href={repositoryPath(locale, owner, repository, "labels")}>
            {dictionary.issuesPage.labelsButton}
          </Link>
          <Link className={styles.secondaryButton} href={repositoryMilestonesPath(locale, owner, repository)}>
            {dictionary.issuesPage.milestonesButton}
          </Link>
          {!archived && (
            <Link className={styles.primaryButton} href={`${repositoryPath(locale, owner, repository, "issues")}/new`}>
              {dictionary.issuesPage.newIssue}
            </Link>
          )}
        </div>
      }
      title={dictionary.issuesPage.title}
    >
      {query.created === "1" && (
        <FlashNotice
          body={dictionary.issuesPage.createdNotice}
          icon={<CircleAlert aria-hidden="true" size={18} />}
          title={dictionary.issuesPage.createdNotice}
          tone="success"
        />
      )}
      <div className={styles.workItemList}>
        <WorkItemListToolbar
          basePath={basePath}
          closedCount={issues.ok ? issues.data.closedCount : undefined}
          dictionary={dictionary}
          kind="issues"
          openCount={issues.ok ? issues.data.openCount : undefined}
          query={filters}
        />
        {issues.ok ? (
          <IssueList
            dictionary={dictionary}
            issues={issues.data.issues}
            locale={locale}
            owner={owner}
            repository={repository}
          />
        ) : (
          <EmptyState
            body={dictionary.home.apiUnavailableBody}
            icon={<ServerOff aria-hidden="true" />}
            title={dictionary.repository.unavailable}
            tone="warning"
          />
        )}
      </div>
      {issues.ok && (
        <WorkItemListPagination
          basePath={basePath}
          dictionary={dictionary}
          hasNext={issues.data.hasNext}
          page={issues.data.page}
          perPage={issues.data.perPage}
          query={filters}
          totalCount={issues.data.totalCount}
        />
      )}
    </RepositorySection>
  );
}
