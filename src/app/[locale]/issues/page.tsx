import { AuthRequired } from "@/components/auth/auth-required";
import { GlobalWorkItemList } from "@/components/work-items/global-work-item-list";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { globalWorkItemQuery } from "@/lib/global-work-items";
import { getGlobalIssues } from "@/lib/lorehub-api";

type GlobalIssuesPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
};

export const dynamic = "force-dynamic";

export default async function GlobalIssuesPage({ params, searchParams }: GlobalIssuesPageProps) {
  const [{ locale: value }, input] = await Promise.all([params, searchParams]);
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const query = globalWorkItemQuery("issue", input);
  const result = session.status === "authenticated" ? await getGlobalIssues(query) : null;
  if (session.status !== "authenticated") {
    return <AuthRequired dictionary={dictionary} returnTo={`/${locale}/issues`} session={session} />;
  }
  return (
    <GlobalWorkItemList
      dictionary={dictionary}
      kind="issue"
      locale={locale}
      page={result?.ok ? result.data : null}
      query={query}
      unavailable={Boolean(result && !result.ok)}
    />
  );
}
