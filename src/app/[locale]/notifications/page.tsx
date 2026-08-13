import { AuthRequired } from "@/components/auth/auth-required";
import { NotificationInbox } from "@/components/notifications/notification-inbox";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getNotifications } from "@/lib/lorehub-api";

import styles from "./notifications.module.css";

type NotificationsPageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function NotificationsPage({ params }: NotificationsPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  if (session.status !== "authenticated") {
    return (
      <div className={styles.page}>
        <AuthRequired dictionary={dictionary} returnTo={`/${locale}/notifications`} session={session} />
      </div>
    );
  }
  const notifications = await getNotifications();
  return (
    <div className={styles.page}>
      <header className={styles.heading}>
        <h1>{dictionary.notificationsPage.title}</h1>
        <p>{dictionary.notificationsPage.description}</p>
      </header>
      <NotificationInbox
        dictionary={dictionary}
        initialItems={notifications.ok ? notifications.data : []}
        locale={locale}
        session={session}
      />
    </div>
  );
}
