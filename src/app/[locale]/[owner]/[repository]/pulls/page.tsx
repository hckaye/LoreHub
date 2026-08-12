import { CircleAlert, ServerOff } from "lucide-react";
import Link from "next/link";

import { MergeRequestList } from "@/components/repositories/merge-request-list";
import { RepositorySection } from "@/components/repositories/repository-section";
import { WorkItemListPagination } from "@/components/repositories/work-item-list-pagination";
import { WorkItemListToolbar } from "@/components/repositories/work-item-list-toolbar";
import { EmptyState } from "@/components/ui/empty-state";
import { FilterTabs } from "@/components/ui/filter-tabs";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getMergeRequests, getPublicRepository } from "@/lib/lorehub-api";
import {
  parseRepositoryMergeRequestQuery,
  repositoryWorkItemSearchParams,
  type MergeRequestFilter,
  type RawRepositoryWorkItemSearchParams,
  type RepositoryMergeRequestQuery,
} from "@/lib/repository-work-item-query";
import { repositoryPath } from "@/lib/routes";

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
  const state = filters.state ?? "open";
  const [dictionary, mergeRequests, repositoryResult] = await Promise.all([
    getDictionary(locale),
    getMergeRequests(owner, repository, filters),
    getPublicRepository(owner, repository),
  ]);
  const archived = repositoryResult.ok && repositoryResult.data.archivedAt !== null;
  const basePath = repositoryPath(locale, owner, repository, "pulls");
  return (
    <RepositorySection
      actions={
        archived ? undefined : (
          <Link className={styles.primaryButton} href={`${basePath}/new`}>
            {dictionary.pullRequestsPage.newPullRequest}
          </Link>
        )
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
      <WorkItemListToolbar
        basePath={basePath}
        dictionary={dictionary}
        kind="pulls"
        query={filters}
        totalCount={mergeRequests.ok ? mergeRequests.data.totalCount : undefined}
      />
      <div className={styles.workItemList}>
        <FilterTabs
          label={dictionary.pullRequestsPage.filterLabel}
          tabs={[
            {
              active: state === "open",
              count: mergeRequests.ok ? mergeRequests.data.openCount : undefined,
              href: stateHref(basePath, filters, "open"),
              label: dictionary.common.open,
            },
            {
              active: state === "closed",
              count: mergeRequests.ok ? mergeRequests.data.closedCount : undefined,
              href: stateHref(basePath, filters, "closed"),
              label: dictionary.common.closed,
            },
            {
              active: state === "merged",
              count: mergeRequests.ok ? mergeRequests.data.mergedCount : undefined,
              href: stateHref(basePath, filters, "merged"),
              label: dictionary.common.merged,
            },
            {
              active: state === "all",
              count: mergeRequests.ok
                ? mergeRequests.data.openCount + mergeRequests.data.closedCount + mergeRequests.data.mergedCount
                : undefined,
              href: stateHref(basePath, filters, "all"),
              label: dictionary.common.all,
            },
          ]}
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
          query={filters}
        />
      )}
    </RepositorySection>
  );
}

function stateHref(basePath: string, query: RepositoryMergeRequestQuery, state: MergeRequestFilter): string {
  const params = repositoryWorkItemSearchParams(query, { state, page: 1 });
  return params.size > 0 ? `${basePath}?${params}` : basePath;
}
