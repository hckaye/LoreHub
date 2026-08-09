import { CircleUserRound } from "lucide-react";

import { AuthRequired } from "@/components/auth/auth-required";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";

import styles from "@/components/account/account-details.module.css";

type ProfilePageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function ProfilePage({ params }: ProfilePageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  if (session.status !== "authenticated") {
    return (
      <div className="account-page">
        <RepositorySection description={dictionary.profile.description} title={dictionary.profile.title}>
          <AuthRequired dictionary={dictionary} returnTo={`/${locale}/profile`} session={session} />
        </RepositorySection>
      </div>
    );
  }
  const { user } = session;
  return (
    <div className="account-page">
      <RepositorySection description={dictionary.profile.description} title={dictionary.profile.title}>
        <div className={styles.session}>
          <CircleUserRound aria-hidden="true" size={18} />
          {dictionary.profile.active}
        </div>
        <dl className={styles.details}>
          <div>
            <dt>{dictionary.profile.username}</dt>
            <dd>@{user.username}</dd>
          </div>
          <div>
            <dt>{dictionary.profile.displayName}</dt>
            <dd>{user.displayName}</dd>
          </div>
          <div>
            <dt>{dictionary.profile.email}</dt>
            <dd>{user.email ?? dictionary.insightsPage.metricUnavailable}</dd>
          </div>
          <div>
            <dt>{dictionary.profile.locale}</dt>
            <dd>{user.locale ?? locale}</dd>
          </div>
        </dl>
        <EmptyState
          body={dictionary.profile.sessionBody}
          icon={<CircleUserRound aria-hidden="true" />}
          title={dictionary.profile.sessionTitle}
        />
      </RepositorySection>
    </div>
  );
}
