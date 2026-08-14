import { AccountSettingsHeader } from "@/components/account/account-settings-shell";
import { PersonalAccessTokenCreateForm } from "@/components/account/personal-access-token-create-form";
import { AuthRequired } from "@/components/auth/auth-required";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { accountSettingsPath } from "@/lib/routes";

type NewPersonalAccessTokenPageProps = {
  params: Promise<{ locale: string }>;
};

export default async function NewPersonalAccessTokenPage({ params }: NewPersonalAccessTokenPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.personalAccessTokens;
  if (session.status !== "authenticated") {
    return (
      <AuthRequired dictionary={dictionary} returnTo={accountSettingsPath(locale, "tokens/new")} session={session} />
    );
  }
  return (
    <>
      <AccountSettingsHeader title={copy.newTitle} />
      <PersonalAccessTokenCreateForm dictionary={dictionary} locale={locale} session={session} />
    </>
  );
}
