import { AuthRequired } from "@/components/auth/auth-required";
import { NotificationInbox } from "@/components/notifications/notification-inbox";
import { RepositorySection } from "@/components/repositories/repository-section";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getNotifications } from "@/lib/lorehub-api";

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
      <RepositorySection
        description={dictionary.notificationsPage.description}
        title={dictionary.notificationsPage.title}
      >
        <AuthRequired dictionary={dictionary} returnTo={`/${locale}/notifications`} session={session} />
      </RepositorySection>
    );
  }
  const notifications = await getNotifications();
  return (
    <RepositorySection
      description={dictionary.notificationsPage.description}
      title={dictionary.notificationsPage.title}
    >
      <NotificationInbox
        dictionary={dictionary}
        initialItems={notifications.ok ? notifications.data : []}
        locale={locale}
        session={session}
      />
    </RepositorySection>
  );
}
