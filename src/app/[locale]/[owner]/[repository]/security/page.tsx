import { LockKeyhole, ServerOff, ShieldCheck } from "lucide-react";

import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { StatusBadge } from "@/components/ui/status-badge";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getPublicRepository } from "@/lib/lorehub-api";

import styles from "@/components/repositories/repository-detail.module.css";

type SecurityPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function SecurityPage({ params }: SecurityPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, repositoryResult] = await Promise.all([
    getDictionary(locale),
    getPublicRepository(owner, repository),
  ]);
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
  return (
    <RepositorySection description={dictionary.securityPage.description} title={dictionary.securityPage.title}>
      <RepositoryPanel description={dictionary.securityPage.accessBody} title={dictionary.securityPage.accessTitle}>
        <div className={styles.accessRow}>
          {repositoryResult.data.visibility === "public" ? (
            <ShieldCheck aria-hidden="true" size={18} />
          ) : (
            <LockKeyhole aria-hidden="true" size={18} />
          )}
          <span>{dictionary.settingsPage.visibility}</span>
          <StatusBadge>{dictionary.common[repositoryResult.data.visibility]}</StatusBadge>
        </div>
      </RepositoryPanel>
      <EmptyState
        body={dictionary.securityPage.emptyBody}
        icon={<ShieldCheck aria-hidden="true" />}
        title={dictionary.securityPage.emptyTitle}
      />
    </RepositorySection>
  );
}
