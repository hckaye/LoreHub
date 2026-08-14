import { CircleAlert, ServerOff } from "lucide-react";

import { MergeRequestList } from "@/components/repositories/merge-request-list";
import { WorkItemListPagination } from "@/components/repositories/work-item-list-pagination";
import { WorkItemListFilterHeader, WorkItemListToolbar } from "@/components/repositories/work-item-list-toolbar";
import { EmptyState } from "@/components/ui/empty-state";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getLabels, getMergeRequests, getMilestones, getPublicRepository } from "@/lib/lorehub-api";
import {
  type RawRepositoryWorkItemSearchParams,
  parseRepositoryMergeRequestQuery,
} from "@/lib/repository-work-item-query";
import { repositoryMilestonesPath, repositoryPath } from "@/lib/routes";

import styles from "@/components/repositories/repository-section.module.css";

type PullRequestsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<RawRepositoryWorkItemSearchParams & { created?: string }>;
};

export const dynamic = "force-dynamic";

export default async function PullRequestsPage({ params, searchParams }: PullRequestsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const filters = parseRepositoryMergeRequestQuery(query);
  const [dictionary, mergeRequests, repositoryResult, labelsResult, milestonesResult] = await Promise.all([
    getDictionary(locale),
    getMergeRequests(owner, repository, filters),
    getPublicRepository(owner, repository),
    getLabels(owner, repository),
    getMilestones(owner, repository, "open", 1, 100),
  ]);
  const archived = repositoryResult.ok && repositoryResult.data.archivedAt !== null;
  const basePath = repositoryPath(locale, owner, repository, "pulls");
  return (
    <div className={styles.page}>
      {query.created === "1" && (
        <FlashNotice
          body={dictionary.pullRequestsPage.createdNotice}
          icon={<CircleAlert aria-hidden="true" size={18} />}
          title={dictionary.pullRequestsPage.createdNotice}
          tone="success"
        />
      )}
      <WorkItemListToolbar
        basePath={basePath}
        createHref={pullRequestCreateHref(basePath, !archived)}
        createLabel={dictionary.pullRequestsPage.newPullRequest}
        dictionary={dictionary}
        kind="pulls"
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
          closedCount={mergeRequests.ok ? mergeRequests.data.closedCount : undefined}
          dictionary={dictionary}
          kind="pulls"
          labels={labelsResult.ok ? labelsResult.data : []}
          mergedCount={mergeRequests.ok ? mergeRequests.data.mergedCount : undefined}
          milestones={milestonesResult.ok ? milestonesResult.data.milestones : []}
          openCount={mergeRequests.ok ? mergeRequests.data.openCount : undefined}
          query={filters}
        />
        {mergeRequests.ok ? (
          <MergeRequestList
            dictionary={dictionary}
            locale={locale}
            mergeRequests={mergeRequests.data.mergeRequests}
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
      {mergeRequests.ok && (
        <WorkItemListPagination
          basePath={basePath}
          dictionary={dictionary}
          hasNext={mergeRequests.data.hasNext}
          page={mergeRequests.data.page}
          perPage={mergeRequests.data.perPage}
          query={filters}
          totalCount={mergeRequests.data.totalCount}
        />
      )}
    </div>
  );
}

function pullRequestCreateHref(basePath: string, canCreate: boolean): string | undefined {
  return canCreate ? `${basePath}/new` : undefined;
}
