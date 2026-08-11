import { BarChart3, LockKeyhole, ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { RepositoryInsightsView } from "@/components/repositories/repository-insights";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getRepositoryInsights } from "@/lib/lorehub-api";

type InsightsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ days?: string | string[] }>;
};

export const dynamic = "force-dynamic";

export default async function InsightsPage({ params, searchParams }: InsightsPageProps) {
  const [{ locale: value, owner, repository }, search] = await Promise.all([params, searchParams]);
  const locale = isLocale(value) ? value : "en";
  const dictionary = await getDictionary(locale);
  const days = search.days === undefined ? "30" : typeof search.days === "string" ? search.days : "invalid";
  const result = await getRepositoryInsights(owner, repository, days);

  if (!result.ok && result.reason === "not-found") notFound();
  return (
    <RepositorySection description={dictionary.insightsPage.description} title={dictionary.insightsPage.title}>
      {result.ok ? (
        <RepositoryInsightsView
          data={result.data}
          dictionary={dictionary}
          locale={locale}
          owner={owner}
          repository={repository}
        />
      ) : result.reason === "forbidden" ? (
        <EmptyState
          body={dictionary.insightsPage.forbiddenBody}
          icon={<LockKeyhole aria-hidden="true" />}
          title={dictionary.insightsPage.forbiddenTitle}
          tone="warning"
        />
      ) : result.reason === "invalid" ? (
        <EmptyState
          body={dictionary.insightsPage.invalidPeriod}
          icon={<BarChart3 aria-hidden="true" />}
          title={dictionary.insightsPage.unavailableTitle}
          tone="warning"
        />
      ) : (
        <EmptyState
          body={dictionary.insightsPage.unavailableBody}
          icon={<ServerOff aria-hidden="true" />}
          title={dictionary.insightsPage.unavailableTitle}
          tone="warning"
        />
      )}
    </RepositorySection>
  );
}
