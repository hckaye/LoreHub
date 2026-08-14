import Link from "next/link";

import { AccountSettingsHeader } from "@/components/account/account-settings-shell";
import chrome from "@/components/account/account-settings.module.css";
import { PersonalAccessTokenList } from "@/components/account/personal-access-token-list";
import { AuthRequired } from "@/components/auth/auth-required";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getPersonalAccessTokens } from "@/lib/lorehub-api";
import { accountSettingsPath } from "@/lib/routes";

type PersonalAccessTokenListPageProps = {
  params: Promise<{ locale: string }>;
};

export default async function PersonalAccessTokenListPage({ params }: PersonalAccessTokenListPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.personalAccessTokens;
  if (session.status !== "authenticated") {
    return <AuthRequired dictionary={dictionary} returnTo={accountSettingsPath(locale, "tokens")} session={session} />;
  }
  const tokens = await getPersonalAccessTokens();
  return (
    <>
      <AccountSettingsHeader
        actions={
          <Link className={chrome.primaryLink} href={accountSettingsPath(locale, "tokens/new")}>
            {copy.generateNew}
          </Link>
        }
        description={copy.description}
        title={copy.title}
      />
      {tokens.ok ? (
        <PersonalAccessTokenList
          dictionary={dictionary}
          initialTokens={tokens.data.tokens}
          locale={locale}
          session={session}
        />
      ) : (
        <FlashNotice body={copy.unavailable} title={dictionary.errors.unavailable} tone="warning" />
      )}
    </>
  );
}
