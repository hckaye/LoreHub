import { AuthRequired } from "@/components/auth/auth-required";
import { CreatePage } from "@/components/create/create-page";
import { RegisterRepositoryForm } from "@/components/repositories/register-repository-form";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getDashboard } from "@/lib/lorehub-api";

type NewRepositoryPageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function NewRepositoryPage({ params }: NewRepositoryPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const dashboard = session.status === "authenticated" ? await getDashboard() : null;
  const copy = dictionary.createPages;
  return (
    <CreatePage description={copy.repositoryIntro} title={copy.repositoryTitle}>
      {session.status === "authenticated" ? (
        <RegisterRepositoryForm
          dictionary={dictionary}
          locale={locale}
          organizations={dashboard?.ok ? dashboard.data.organizations : []}
          session={session}
        />
      ) : (
        <AuthRequired dictionary={dictionary} returnTo={`/${locale}/repositories/new`} session={session} />
      )}
    </CreatePage>
  );
}
