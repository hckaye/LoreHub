import { AccountSettingsHeader } from "@/components/account/account-settings-shell";
import { ProfileSettingsForm } from "@/components/account/profile-settings-form";
import { AuthRequired } from "@/components/auth/auth-required";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getUserProfile } from "@/lib/lorehub-api";
import { accountSettingsPath } from "@/lib/routes";

type ProfileSettingsPageProps = {
  params: Promise<{ locale: string }>;
};

export default async function ProfileSettingsPage({ params }: ProfileSettingsPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.accountSettings;
  if (session.status !== "authenticated") {
    return <AuthRequired dictionary={dictionary} returnTo={accountSettingsPath(locale, "profile")} session={session} />;
  }
  const profile = await getUserProfile(session.user.username);
  return (
    <>
      <AccountSettingsHeader title={copy.profileTitle} />
      {profile.ok ? (
        <ProfileSettingsForm dictionary={dictionary} profile={profile.data} session={session} />
      ) : (
        <FlashNotice body={copy.profileUnavailable} title={dictionary.errors.unavailable} tone="warning" />
      )}
    </>
  );
}
