import { notFound } from "next/navigation";

import { SearchPage } from "@/components/search/search-page";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getSearchResults, type SearchType } from "@/lib/lorehub-api";

type SearchRouteProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ q?: string; type?: string }>;
};

export const dynamic = "force-dynamic";

export default async function SearchRoute({ params, searchParams }: SearchRouteProps) {
  const { locale: value } = await params;
  if (!isLocale(value)) {
    notFound();
  }
  const [{ q = "", type = "all" }, dictionary] = await Promise.all([searchParams, getDictionary(value)]);
  const searchType: SearchType = ["organizations", "repositories", "users"].includes(type)
    ? (type as SearchType)
    : "all";
  const result = q.trim() ? await getSearchResults(q, searchType) : null;
  return <SearchPage dictionary={dictionary} locale={value} query={q} results={result?.ok ? result.data : null} />;
}
