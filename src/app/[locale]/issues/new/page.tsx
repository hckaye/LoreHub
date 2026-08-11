import { AuthRequired } from "@/components/auth/auth-required";
import { RepositoryChooser } from "@/components/repositories/repository-chooser";
import { RepositorySection } from "@/components/repositories/repository-section";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getDashboard } from "@/lib/lorehub-api";

type NewIssueChooserPageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function NewIssueChooserPage({ params }: NewIssueChooserPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const dashboard = session.status === "authenticated" ? await getDashboard() : null;
  return (
    <RepositorySection description={dictionary.forms.chooseRepositoryBody} title={dictionary.forms.chooseRepository}>
      {session.status === "authenticated" ? (
        <RepositoryChooser
          dictionary={dictionary}
          locale={locale}
          repositories={dashboard?.ok ? dashboard.data.repositories : null}
          section="issues"
          unavailable={dashboard ? !dashboard.ok : false}
        />
      ) : (
        <AuthRequired dictionary={dictionary} returnTo={`/${locale}/issues/new`} session={session} />
      )}
    </RepositorySection>
  );
}
