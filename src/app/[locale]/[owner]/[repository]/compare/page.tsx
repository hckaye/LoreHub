import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { DiffView } from "@/components/repositories/diff-view";
import { RepositoryPanel } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getLoreDiff, getPublicRepository } from "@/lib/lorehub-api";

type ComparePageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ source?: string; target?: string }>;
};

export const dynamic = "force-dynamic";

export default async function ComparePage({ params, searchParams }: ComparePageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const [dictionary, repository] = await Promise.all([getDictionary(locale), getPublicRepository(owner, slug)]);
  if (!repository.ok && repository.reason === "not-found") notFound();
  if (!repository.ok || !query.source || !query.target) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.compare}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  const diff = await getLoreDiff(owner, slug, query.source, query.target);
  if (!diff.ok) {
    return (
      <EmptyState
        body={dictionary.codeBrowser.unavailable}
        icon={<ServerOff />}
        title={dictionary.repository.unavailable}
      />
    );
  }
  return (
    <RepositoryPanel description={`${query.source} → ${query.target}`} title={dictionary.codeBrowser.compare}>
      <DiffView diff={diff.data} dictionary={dictionary} />
    </RepositoryPanel>
  );
}
