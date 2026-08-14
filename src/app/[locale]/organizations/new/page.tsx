import { AuthRequired } from "@/components/auth/auth-required";
import { CreatePage } from "@/components/create/create-page";
import { OrganizationForm } from "@/components/organizations/organization-form";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";

type NewOrganizationPageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function NewOrganizationPage({ params }: NewOrganizationPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.createPages;
  return (
    <CreatePage description={copy.organizationIntro} title={copy.organizationTitle}>
      {session.status === "authenticated" ? (
        <OrganizationForm dictionary={dictionary} locale={locale} session={session} />
      ) : (
        <AuthRequired dictionary={dictionary} returnTo={`/${locale}/organizations/new`} session={session} />
      )}
    </CreatePage>
  );
}
