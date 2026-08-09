import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { RepositoryPanel } from "@/components/repositories/repository-section";
import { RevisionHistory } from "@/components/repositories/revision-history";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getPublicRepository, getRevisionHistory } from "@/lib/lorehub-api";

type HistoryPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ branch?: string; revision?: string }>;
};

export const dynamic = "force-dynamic";

export default async function HistoryPage({ params, searchParams }: HistoryPageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const [dictionary, repository] = await Promise.all([getDictionary(locale), getPublicRepository(owner, slug)]);
  if (!repository.ok && repository.reason === "not-found") notFound();
  if (!repository.ok) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const history = await getRevisionHistory(owner, slug, { branch: query.branch, revision: query.revision });
  if (!history.ok) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  return (
    <RepositoryPanel description={dictionary.codeBrowser.history} title={dictionary.codeBrowser.history}>
      <RevisionHistory
        dictionary={dictionary}
        entries={history.data.entries}
        locale={locale}
        owner={owner}
        repository={slug}
      />
    </RepositoryPanel>
  );
}
