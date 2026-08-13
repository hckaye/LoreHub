import { CircleAlert, ServerOff } from "lucide-react";
import Link from "next/link";

import { MergeRequestList } from "@/components/repositories/merge-request-list";
import { RepositorySection } from "@/components/repositories/repository-section";
import { WorkItemListPagination } from "@/components/repositories/work-item-list-pagination";
import { WorkItemListToolbar } from "@/components/repositories/work-item-list-toolbar";
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
    <RepositorySection
      actions={
        <div className={styles.panelActions}>
          <Link className={styles.secondaryButton} href={repositoryPath(locale, owner, repository, "labels")}>
            {dictionary.pullRequestsPage.labelsButton}
          </Link>
          <Link className={styles.secondaryButton} href={repositoryMilestonesPath(locale, owner, repository)}>
            {dictionary.pullRequestsPage.milestonesButton}
          </Link>
          {!archived && (
            <Link className={styles.primaryButton} href={`${basePath}/new`}>
              {dictionary.pullRequestsPage.newPullRequest}
            </Link>
          )}
        </div>
      }
      title={dictionary.pullRequestsPage.title}
    >
      {query.created === "1" && (
        <FlashNotice
          body={dictionary.pullRequestsPage.createdNotice}
          icon={<CircleAlert aria-hidden="true" size={18} />}
          title={dictionary.pullRequestsPage.createdNotice}
          tone="success"
        />
      )}
      <div className={styles.workItemList}>
        <WorkItemListToolbar
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
    </RepositorySection>
  );
}
