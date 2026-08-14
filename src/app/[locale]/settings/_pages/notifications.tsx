import { AccountSettingsHeader } from "@/components/account/account-settings-shell";
import { NotificationSettingsForm } from "@/components/account/notification-settings-form";
import { AuthRequired } from "@/components/auth/auth-required";
import { FlashNotice } from "@/components/ui/flash-notice";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getNotificationPreferences } from "@/lib/lorehub-api";
import { accountSettingsPath } from "@/lib/routes";

type NotificationSettingsPageProps = {
  params: Promise<{ locale: string }>;
};

export default async function NotificationSettingsPage({ params }: NotificationSettingsPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.accountSettings;
  if (session.status !== "authenticated") {
    return (
      <AuthRequired dictionary={dictionary} returnTo={accountSettingsPath(locale, "notifications")} session={session} />
    );
  }
  const preferences = await getNotificationPreferences();
  return (
    <>
      <AccountSettingsHeader description={copy.notificationBody} title={copy.notificationTitle} />
      {preferences.ok ? (
        <NotificationSettingsForm dictionary={dictionary} preferences={preferences.data} session={session} />
      ) : (
        <FlashNotice body={copy.notificationsUnavailable} title={dictionary.errors.unavailable} tone="warning" />
      )}
    </>
  );
}
