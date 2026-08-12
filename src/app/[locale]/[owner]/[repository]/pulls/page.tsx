import { CircleAlert, ServerOff } from "lucide-react";
import Link from "next/link";

import { MergeRequestList } from "@/components/repositories/merge-request-list";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { FilterTabs } from "@/components/ui/filter-tabs";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getMergeRequests, getPublicRepository, type MergeRequestFilter } from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

import styles from "@/components/repositories/repository-section.module.css";

type PullRequestsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ state?: string; created?: string }>;
};

export const dynamic = "force-dynamic";

export default async function PullRequestsPage({ params, searchParams }: PullRequestsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const state = parseMergeRequestFilter(query.state);
  const [dictionary, mergeRequests, repositoryResult] = await Promise.all([
    getDictionary(locale),
    getMergeRequests(owner, repository, state),
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
      description={dictionary.pullRequestsPage.description}
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
      <FilterTabs
        label={dictionary.pullRequestsPage.filterLabel}
        tabs={[
          { active: state === "open", href: basePath, label: dictionary.common.open },
          { active: state === "closed", href: `${basePath}?state=closed`, label: dictionary.common.closed },
          { active: state === "merged", href: `${basePath}?state=merged`, label: dictionary.common.merged },
          { active: state === "all", href: `${basePath}?state=all`, label: dictionary.common.all },
        ]}
      />
      <RepositoryPanel
        description={dictionary.repository.pullRequestsDescription}
        title={dictionary.repository.pullRequestsTitle}
      >
        {mergeRequests.ok ? (
          <MergeRequestList
            dictionary={dictionary}
            locale={locale}
            mergeRequests={mergeRequests.data}
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
    </RepositorySection>
  );
}

function parseMergeRequestFilter(value: string | undefined): MergeRequestFilter {
  if (value === "closed" || value === "merged" || value === "all") {
    return value;
  }
  return "open";
}
