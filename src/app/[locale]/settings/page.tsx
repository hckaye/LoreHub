import { Languages, Settings2 } from "lucide-react";

import { AuthRequired } from "@/components/auth/auth-required";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";

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
  return (
    <RepositorySection description={dictionary.accountSettings.description} title={dictionary.accountSettings.title}>
      <RepositoryPanel
        description={dictionary.accountSettings.identityBody}
        title={dictionary.accountSettings.identityTitle}
      >
        <div className={styles.readOnly}>
          <Settings2 aria-hidden="true" size={18} />
          {dictionary.common.readOnly}
        </div>
        <dl className={styles.details}>
          <div>
            <dt>{dictionary.profile.username}</dt>
            <dd>@{session.user.username}</dd>
          </div>
          <div>
            <dt>{dictionary.profile.displayName}</dt>
            <dd>{session.user.displayName}</dd>
          </div>
          <div>
            <dt>{dictionary.profile.email}</dt>
            <dd>{session.user.email ?? dictionary.insightsPage.metricUnavailable}</dd>
          </div>
        </dl>
      </RepositoryPanel>
      <RepositoryPanel
        description={dictionary.accountSettings.localeBody}
        title={dictionary.accountSettings.localeTitle}
      >
        <div className={styles.readOnly}>
          <Languages aria-hidden="true" size={18} />
          {locale}
        </div>
      </RepositoryPanel>
    </RepositorySection>
  );
}
