import { KeyRound, Languages, Settings2 } from "lucide-react";

import { NotificationSettingsForm } from "@/components/account/notification-settings-form";
import { PersonalAccessTokenSettings } from "@/components/account/personal-access-token-settings";
import { ProfileSettingsForm } from "@/components/account/profile-settings-form";
import { AuthRequired } from "@/components/auth/auth-required";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getNotificationPreferences, getPersonalAccessTokens, getUserProfile } from "@/lib/lorehub-api";

import styles from "@/components/repositories/repository-detail.module.css";

type AccountSettingsPageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function AccountSettingsPage({ params }: AccountSettingsPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  if (session.status !== "authenticated") {
    return (
      <RepositorySection description={dictionary.accountSettings.description} title={dictionary.accountSettings.title}>
        <AuthRequired dictionary={dictionary} returnTo={`/${locale}/settings`} session={session} />
      </RepositorySection>
    );
  }
  const [profile, preferences, personalAccessTokens] = await Promise.all([
    getUserProfile(session.user.username),
    getNotificationPreferences(),
    getPersonalAccessTokens(),
  ]);
  return (
    <RepositorySection description={dictionary.accountSettings.description} title={dictionary.accountSettings.title}>
      <RepositoryPanel
        description={dictionary.accountSettings.profileBody}
        title={dictionary.accountSettings.profileTitle}
      >
        {profile.ok ? (
          <ProfileSettingsForm dictionary={dictionary} profile={profile.data} session={session} />
        ) : (
          <div className={styles.readOnly}>
            <Settings2 aria-hidden="true" size={18} />
            {dictionary.common.readOnly}
          </div>
        )}
      </RepositoryPanel>
      <RepositoryPanel
        description={dictionary.accountSettings.notificationBody}
        title={dictionary.accountSettings.notificationTitle}
      >
        {preferences.ok ? (
          <NotificationSettingsForm dictionary={dictionary} preferences={preferences.data} session={session} />
        ) : (
          <div className={styles.readOnly}>
            <Languages aria-hidden="true" size={18} />
            {dictionary.common.readOnly}
          </div>
        )}
      </RepositoryPanel>
      <RepositoryPanel
        description={dictionary.personalAccessTokens.description}
        title={dictionary.personalAccessTokens.title}
      >
        {personalAccessTokens.ok ? (
          <PersonalAccessTokenSettings
            dictionary={dictionary}
            initialTokens={personalAccessTokens.data.tokens}
            locale={locale}
            session={session}
          />
        ) : (
          <div className={styles.readOnly}>
            <KeyRound aria-hidden="true" size={18} />
            {dictionary.personalAccessTokens.unavailable}
          </div>
        )}
      </RepositoryPanel>
    </RepositorySection>
  );
}
