import { AccountSettingsHeader } from "@/components/account/account-settings-shell";
import { RepositoryInvitationSettings } from "@/components/account/repository-invitation-settings";
import { AuthRequired } from "@/components/auth/auth-required";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { accountSettingsPath } from "@/lib/routes";

type RepositoryInvitationSettingsPageProps = {
  params: Promise<{ locale: string }>;
};

export default async function RepositoryInvitationSettingsPage({ params }: RepositoryInvitationSettingsPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.repositoryInvitations;
  if (session.status !== "authenticated") {
    return (
      <AuthRequired dictionary={dictionary} returnTo={accountSettingsPath(locale, "repositories")} session={session} />
    );
  }
  return (
    <>
      <AccountSettingsHeader description={copy.description} title={copy.title} />
      <RepositoryInvitationSettings dictionary={dictionary} locale={locale} session={session} />
    </>
  );
}
