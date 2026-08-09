import { CircleAlert, ServerOff } from "lucide-react";
import Link from "next/link";

import { IssueList } from "@/components/repositories/issue-list";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { FilterTabs } from "@/components/ui/filter-tabs";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getIssues, type IssueFilter } from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

import styles from "@/components/repositories/repository-section.module.css";

type IssuesPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ state?: string; created?: string }>;
};

export const dynamic = "force-dynamic";

export default async function IssuesPage({ params, searchParams }: IssuesPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const state = parseIssueFilter(query.state);
  const [dictionary, issues] = await Promise.all([getDictionary(locale), getIssues(owner, repository, state)]);
  return (
    <RepositorySection
      actions={
        <Link className={styles.primaryButton} href={`${repositoryPath(locale, owner, repository, "issues")}/new`}>
          {dictionary.issuesPage.newIssue}
        </Link>
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
      <FilterTabs
        label={dictionary.issuesPage.filterLabel}
        tabs={[
          {
            active: state === "open",
            href: repositoryPath(locale, owner, repository, "issues"),
            label: dictionary.common.open,
          },
          {
            active: state === "closed",
            href: `${repositoryPath(locale, owner, repository, "issues")}?state=closed`,
            label: dictionary.common.closed,
          },
          {
            active: state === "all",
            href: `${repositoryPath(locale, owner, repository, "issues")}?state=all`,
            label: dictionary.common.all,
          },
        ]}
      />
      <RepositoryPanel description={dictionary.issuesPage.description} title={dictionary.repository.issuesTitle}>
        {issues.ok ? (
          <IssueList dictionary={dictionary} issues={issues.data} locale={locale} />
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

function parseIssueFilter(value: string | undefined): IssueFilter {
  return value === "closed" || value === "all" ? value : "open";
}
