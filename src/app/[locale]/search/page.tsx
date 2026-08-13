import { notFound, redirect } from "next/navigation";

import { SearchPage } from "@/components/search/search-page";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getSearchResults } from "@/lib/lorehub-api";
import { lastSearchPage, normalizeSearchQuery, parseCodeSearchQualifier, searchHref } from "@/lib/search";

type SearchRouteProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

export const dynamic = "force-dynamic";

export default async function SearchRoute({ params, searchParams }: SearchRouteProps) {
  const { locale: value } = await params;
  if (!isLocale(value)) {
    notFound();
  }
  const [input, dictionary] = await Promise.all([searchParams, getDictionary(value)]);
  const query = normalizeSearchQuery(input);
  const canSearchCode = query.type !== "code" || Boolean(parseCodeSearchQualifier(query.q));
  const result = query.q && canSearchCode ? await getSearchResults(query.q, query.type, query.page) : null;
  if (result?.ok && query.page > lastSearchPage(result.data, query.type)) {
    redirect(searchHref(value, query, { page: lastSearchPage(result.data, query.type) }));
  }
  const failure = result && !result.ok ? failureReason(result.reason) : null;
  return (
    <SearchPage
      dictionary={dictionary}
      failure={failure}
      locale={value}
      query={query}
      results={result?.ok ? result.data : null}
    />
  );
}

function failureReason(reason: "not-found" | "unauthorized" | "forbidden" | "invalid" | "unavailable") {
  if (reason === "unauthorized" || reason === "forbidden") return "forbidden";
  return reason === "invalid" ? "invalid" : "unavailable";
}
