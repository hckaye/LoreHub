import { CircleAlert, ServerOff } from "lucide-react";

import { IssueList } from "@/components/repositories/issue-list";
import { WorkItemListPagination } from "@/components/repositories/work-item-list-pagination";
import { WorkItemListFilterHeader, WorkItemListToolbar } from "@/components/repositories/work-item-list-toolbar";
import { EmptyState } from "@/components/ui/empty-state";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getIssues, getLabels, getMilestones, getPublicRepository } from "@/lib/lorehub-api";
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
  const [dictionary, issues, repositoryResult, labelsResult, milestonesResult] = await Promise.all([
    getDictionary(locale),
    getIssues(owner, repository, filters),
    getPublicRepository(owner, repository),
    getLabels(owner, repository),
    getMilestones(owner, repository, "open", 1, 100),
  ]);
  const archived = repositoryResult.ok && repositoryResult.data.archivedAt !== null;
  const basePath = repositoryPath(locale, owner, repository, "issues");
  const filterOptions = issueFilterOptions(issues);
  return (
    <div className={styles.page}>
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
        createHref={archived ? undefined : `${basePath}/new`}
        createLabel={dictionary.issuesPage.newIssue}
        dictionary={dictionary}
        kind="issues"
        labels={labelsResult.ok ? labelsResult.data : []}
        labelsCount={labelsResult.ok ? labelsResult.data.length : undefined}
        labelsHref={repositoryPath(locale, owner, repository, "labels")}
        milestones={milestonesResult.ok ? milestonesResult.data.milestones : []}
        milestonesCount={milestonesResult.ok ? milestonesResult.data.milestones.length : undefined}
        milestonesHref={repositoryMilestonesPath(locale, owner, repository)}
        query={filters}
      />
      <div className={styles.workItemList}>
        <WorkItemListFilterHeader
          basePath={basePath}
          closedCount={issues.ok ? issues.data.closedCount : undefined}
          dictionary={dictionary}
          kind="issues"
          labels={labelsResult.ok ? labelsResult.data : []}
          milestones={milestonesResult.ok ? milestonesResult.data.milestones : []}
          openCount={issues.ok ? issues.data.openCount : undefined}
          query={filters}
          authors={filterOptions.authors}
          assignees={filterOptions.assignees}
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
    </div>
  );
}

function issueFilterOptions(result: Awaited<ReturnType<typeof getIssues>>) {
  if (!result.ok) return { authors: [], assignees: [] };
  return {
    authors: result.data.issues.map((issue) => issue.author),
    assignees: result.data.issues.flatMap((issue) => issue.assignees),
  };
}
