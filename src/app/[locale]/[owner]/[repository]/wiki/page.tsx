import { LockKeyhole, ServerOff } from "lucide-react";

import { EmptyState } from "@/components/ui/empty-state";
import { WikiIndex } from "@/components/wiki/wiki-index";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getWikiPages } from "@/lib/lorehub-api";

type WikiPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ q?: string }>;
};

export const dynamic = "force-dynamic";

export default async function WikiPage({ params, searchParams }: WikiPageProps) {
  const [{ locale: value, owner, repository }, query] = await Promise.all([params, searchParams]);
  const locale = isLocale(value) ? value : "en";
  const q = query.q?.trim() ?? "";
  const [dictionary, pages, session] = await Promise.all([
    getDictionary(locale),
    getWikiPages(owner, repository, q),
    getAuthSession(),
  ]);
  const labels = dictionary.wikiPage;
  if (pages.ok) {
    return (
      <WikiIndex
        data={pages.data}
        dictionary={dictionary}
        locale={locale}
        owner={owner}
        query={q}
        repository={repository}
        session={session}
      />
    );
  }
  return pages.reason === "forbidden" || pages.reason === "not-found" ? (
    <EmptyState
      body={labels.forbiddenBody}
      icon={<LockKeyhole aria-hidden="true" />}
      title={labels.forbiddenTitle}
      tone="warning"
    />
  ) : (
    <EmptyState
      body={labels.unavailableBody}
      icon={<ServerOff aria-hidden="true" />}
      title={labels.unavailableTitle}
      tone="warning"
    />
  );
}
