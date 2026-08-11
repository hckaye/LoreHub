import { notFound } from "next/navigation";

import { Dashboard } from "@/components/dashboard/dashboard";
import { PublicExplore } from "@/components/home/public-explore";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";
import { getAuthSession } from "@/lib/auth-api";
import { getDashboard, getPublicRepositories } from "@/lib/lorehub-api";

type HomePageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ q?: string }>;
};

export const dynamic = "force-dynamic";

export default async function HomePage({ params, searchParams }: HomePageProps) {
  const { locale: value } = await params;
  if (!isLocale(value)) {
    notFound();
  }
  const { q = "" } = await searchParams;
  const [dictionary, session] = await Promise.all([getDictionary(value), getAuthSession()]);
  if (session.status === "authenticated") {
    const dashboard = await getDashboard();
    return (
      <Dashboard
        dashboard={dashboard.ok ? dashboard.data : null}
        dictionary={dictionary}
        locale={value}
        unavailable={!dashboard.ok}
        userName={session.user.displayName}
      />
    );
  }
  const repositories = await getPublicRepositories(q);
  const availableRepositories = repositories.ok ? filterRepositories(repositories.data, q) : null;
  return (
    <PublicExplore
      dictionary={dictionary}
      locale={value}
      query={q}
      repositories={availableRepositories}
      unavailable={!repositories.ok}
    />
  );
}

function filterRepositories(repositories: Repository[], query: string): Repository[] {
  const normalized = query.trim().toLocaleLowerCase();
  if (!normalized) {
    return repositories;
  }
  return repositories.filter((repository) =>
    [repository.owner, repository.slug, repository.displayName, repository.description, ...repository.topics]
      .join(" ")
      .toLocaleLowerCase()
      .includes(normalized),
  );
}
