import { CircleAlert, ServerOff } from "lucide-react";
import Link from "next/link";

import { IssueList } from "@/components/repositories/issue-list";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { WorkItemListPagination } from "@/components/repositories/work-item-list-pagination";
import { WorkItemListToolbar } from "@/components/repositories/work-item-list-toolbar";
import { EmptyState } from "@/components/ui/empty-state";
import { FilterTabs } from "@/components/ui/filter-tabs";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getIssues, getPublicRepository } from "@/lib/lorehub-api";
import {
  parseRepositoryIssueQuery,
  repositoryWorkItemSearchParams,
  type IssueFilter,
  type RawRepositoryWorkItemSearchParams,
  type RepositoryIssueQuery,
} from "@/lib/repository-work-item-query";
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
  const state = filters.state ?? "open";
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
            {dictionary.labelsPage.title}
          </Link>
          <Link className={styles.secondaryButton} href={repositoryMilestonesPath(locale, owner, repository)}>
            {dictionary.milestonesPage.milestonesLink}
          </Link>
          {!archived && (
            <Link className={styles.primaryButton} href={`${repositoryPath(locale, owner, repository, "issues")}/new`}>
              {dictionary.issuesPage.newIssue}
            </Link>
          )}
        </div>
      }
      description={dictionary.issuesPage.description}
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
      <WorkItemListToolbar
        basePath={basePath}
        dictionary={dictionary}
        kind="issues"
        query={filters}
        totalCount={issues.ok ? issues.data.totalCount : undefined}
      />
      <FilterTabs
        label={dictionary.issuesPage.filterLabel}
        tabs={[
          {
            active: state === "open",
            count: issues.ok ? issues.data.openCount : undefined,
            href: stateHref(basePath, filters, "open"),
            label: dictionary.common.open,
          },
          {
            active: state === "closed",
            count: issues.ok ? issues.data.closedCount : undefined,
            href: stateHref(basePath, filters, "closed"),
            label: dictionary.common.closed,
          },
          {
            active: state === "all",
            count: issues.ok ? issues.data.openCount + issues.data.closedCount : undefined,
            href: stateHref(basePath, filters, "all"),
            label: dictionary.common.all,
          },
        ]}
      />
      <RepositoryPanel description={dictionary.issuesPage.description} title={dictionary.repository.issuesTitle}>
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
      </RepositoryPanel>
      {issues.ok && (
        <WorkItemListPagination
          basePath={basePath}
          dictionary={dictionary}
          hasNext={issues.data.hasNext}
          page={issues.data.page}
          query={filters}
        />
      )}
    </RepositorySection>
  );
}

function stateHref(basePath: string, query: RepositoryIssueQuery, state: IssueFilter): string {
  const params = repositoryWorkItemSearchParams(query, { state, page: 1 });
  return params.size > 0 ? `${basePath}?${params}` : basePath;
}
