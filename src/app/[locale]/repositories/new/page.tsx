import { AuthRequired } from "@/components/auth/auth-required";
import { RegisterRepositoryForm } from "@/components/repositories/register-repository-form";
import { RepositorySection } from "@/components/repositories/repository-section";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";

type NewRepositoryPageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function NewRepositoryPage({ params }: NewRepositoryPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  return (
    <RepositorySection description={dictionary.home.repositoriesDescription} title={dictionary.common.newRepository}>
      {session.status === "authenticated" ? (
        <RegisterRepositoryForm dictionary={dictionary} locale={locale} session={session} />
      ) : (
        <AuthRequired dictionary={dictionary} returnTo={`/${locale}/repositories/new`} session={session} />
      )}
    </RepositorySection>
  );
}
