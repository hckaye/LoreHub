import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { RepositorySection } from "@/components/repositories/repository-section";
import { RevisionHistory } from "@/components/repositories/revision-history";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getPublicRepository } from "@/lib/lorehub-api";
import { loadRevisionHistoryPage, parseRevisionHistoryPage } from "@/lib/revision-history";

type HistoryPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ branch?: string; page?: string | string[]; revision?: string }>;
};

export const dynamic = "force-dynamic";

export default async function HistoryPage({ params, searchParams }: HistoryPageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const [dictionary, repository] = await Promise.all([getDictionary(locale), getPublicRepository(owner, slug)]);
  if (!repository.ok && repository.reason === "not-found") notFound();
  const historyQuery = { branch: query.branch, revision: query.revision };
  const page = parseRevisionHistoryPage(query.page);
  const history = repository.ok ? await loadRevisionHistoryPage(owner, slug, historyQuery, page) : null;
  if (!history?.ok) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  return (
    <RepositorySection title={dictionary.commitHistory.commits}>
      <RevisionHistory
        basePath={`/${locale}/${encodeURIComponent(owner)}/${encodeURIComponent(slug)}/commits`}
        dictionary={dictionary}
        hasNext={history.data.hasNext}
        locale={locale}
        owner={owner}
        page={history.data.page}
        query={historyQuery}
        repository={slug}
        rows={history.data.rows}
      />
    </RepositorySection>
  );
}
