import { LockKeyhole, ServerOff, Settings2 } from "lucide-react";

import { AuthRequired } from "@/components/auth/auth-required";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getPublicRepository } from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

import styles from "@/components/repositories/repository-detail.module.css";

type RepositorySettingsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function RepositorySettingsPage({ params }: RepositorySettingsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session, repositoryResult] = await Promise.all([
    getDictionary(locale),
    getAuthSession(),
    getPublicRepository(owner, repository),
  ]);
  if (session.status !== "authenticated") {
    return (
      <RepositorySection description={dictionary.settingsPage.description} title={dictionary.settingsPage.title}>
        <AuthRequired
          dictionary={dictionary}
          returnTo={repositoryPath(locale, owner, repository, "settings")}
          session={session}
        />
      </RepositorySection>
    );
  }
  if (!repositoryResult.ok) {
    return (
      <EmptyState
        body={dictionary.home.apiUnavailableBody}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.repository.unavailable}
        tone="warning"
      />
    );
  }
  const data = repositoryResult.data;
  return (
    <RepositorySection description={dictionary.settingsPage.description} title={dictionary.settingsPage.title}>
      <RepositoryPanel description={dictionary.settingsPage.readOnlyBody} title={dictionary.settingsPage.readOnlyTitle}>
        <div className={styles.readOnly}>
          <Settings2 aria-hidden="true" size={18} />
          <span>{dictionary.common.readOnly}</span>
        </div>
      </RepositoryPanel>
      <RepositoryPanel title={dictionary.settingsPage.repositoryIdentity}>
        <dl className={styles.details}>
          <div>
            <dt>{dictionary.settingsPage.owner}</dt>
            <dd>{data.owner}</dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.slug}</dt>
            <dd>{data.slug}</dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.displayName}</dt>
            <dd>{data.displayName}</dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.descriptionLabel}</dt>
            <dd>{data.description || dictionary.common.noDescription}</dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.visibility}</dt>
            <dd>
              <LockKeyhole aria-hidden="true" size={14} /> {dictionary.common[data.visibility]}
            </dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.loreRepositoryId}</dt>
            <dd>
              <code>{data.loreRepositoryId}</code>
            </dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.loreUrl}</dt>
            <dd>
              <code>{data.loreUrl}</code>
            </dd>
          </div>
          <div>
            <dt>{dictionary.settingsPage.defaultBranch}</dt>
            <dd>
              <code>{data.defaultBranch}</code>
            </dd>
          </div>
        </dl>
      </RepositoryPanel>
    </RepositorySection>
  );
}
