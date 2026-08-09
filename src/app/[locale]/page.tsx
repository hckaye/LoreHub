import { notFound } from "next/navigation";

import { Dashboard } from "@/components/dashboard/dashboard";
import { PublicExplore } from "@/components/home/public-explore";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import type { Repository } from "@/lib/api-types";
import { getAuthSession } from "@/lib/auth-api";
import { getPublicRepositories } from "@/lib/lorehub-api";

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
  const [dictionary, repositories, session] = await Promise.all([
    getDictionary(value),
    getPublicRepositories(q),
    getAuthSession(),
  ]);
  const availableRepositories = repositories.ok ? filterRepositories(repositories.data, q) : null;
  if (session.status === "authenticated") {
    return (
      <Dashboard
        dictionary={dictionary}
        locale={value}
        repositories={availableRepositories}
        repositoriesUnavailable={!repositories.ok}
        userName={session.user.displayName}
      />
    );
  }
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
    [repository.owner, repository.slug, repository.displayName, repository.description]
      .join(" ")
      .toLocaleLowerCase()
      .includes(normalized),
  );
}
