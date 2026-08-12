import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { DiscussionDetail } from "@/components/discussions/discussion-detail";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getDiscussion, getDiscussionCategories } from "@/lib/lorehub-api";

type DiscussionDetailPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string; number: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

export const dynamic = "force-dynamic";

export default async function DiscussionDetailPage({ params, searchParams }: DiscussionDetailPageProps) {
  const { locale: value, owner, repository, number: numberValue } = await params;
  const locale = isLocale(value) ? value : "en";
  const number = Number(numberValue);
  const query = await searchParams;
  const commentPage = parseCommentPage(query.comment_page);
  const dictionary = await getDictionary(locale);
  if (!Number.isSafeInteger(number) || number < 1) notFound();
  const [discussion, categories, session] = await Promise.all([
    getDiscussion(owner, repository, number, commentPage),
    getDiscussionCategories(owner, repository),
    getAuthSession(),
  ]);
  if (!discussion.ok && discussion.reason === "not-found") notFound();
  if (!discussion.ok) {
    return (
      <EmptyState
        body={dictionary.home.apiUnavailableBody}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.repository.unavailable}
        tone="warning"
      />
    );
  }
  return (
    <DiscussionDetail
      categories={categories.ok ? categories.data.categories : [discussion.data.category]}
      dictionary={dictionary}
      discussion={discussion.data}
      key={`${discussion.data.id}-${discussion.data.updatedAt}`}
      locale={locale}
      owner={owner}
      repository={repository}
      session={session}
    />
  );
}

function parseCommentPage(value: string | string[] | undefined): number {
  const raw = Array.isArray(value) ? value[0] : value;
  const page = Number(raw);
  return Number.isSafeInteger(page) && page > 0 && page <= 10_000 ? page : 1;
}
