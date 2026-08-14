import { ServerOff } from "lucide-react";

import { DiscussionList } from "@/components/discussions/discussion-list";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getDiscussionCategories, getDiscussions, type DiscussionQuery } from "@/lib/lorehub-api";

type DiscussionsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

export const dynamic = "force-dynamic";

export default async function DiscussionsPage({ params, searchParams }: DiscussionsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const rawQuery = await searchParams;
  const query = parseQuery(rawQuery);
  const [dictionary, categoriesResult, discussions] = await Promise.all([
    getDictionary(locale),
    getDiscussionCategories(owner, repository),
    getDiscussions(owner, repository, query),
  ]);
  return discussions.ok ? (
    <DiscussionList
      categories={categoriesResult.ok ? categoriesResult.data.categories : []}
      dictionary={dictionary}
      locale={locale}
      owner={owner}
      page={discussions.data}
      query={query}
      repository={repository}
    />
  ) : (
    <EmptyState
      body={dictionary.home.apiUnavailableBody}
      icon={<ServerOff aria-hidden="true" />}
      title={dictionary.repository.unavailable}
      tone="warning"
    />
  );
}

function parseQuery(raw: Record<string, string | string[] | undefined>): DiscussionQuery {
  const value = (key: string) => {
    const item = raw[key];
    return Array.isArray(item) ? item[0] : item;
  };
  const state = value("state");
  const sort = value("sort");
  const page = Number(value("page"));
  return {
    category: value("category") || undefined,
    q: value("q") || undefined,
    state: state === "closed" || state === "all" ? state : "open",
    sort: sort === "oldest" || sort === "most-commented" || sort === "most-voted" ? sort : "newest",
    page: Number.isSafeInteger(page) && page > 0 ? page : 1,
  } satisfies DiscussionQuery;
}
