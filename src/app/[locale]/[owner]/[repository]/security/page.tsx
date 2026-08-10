import { LockKeyhole, ServerOff, ShieldCheck } from "lucide-react";

import { CodeScanningDashboard } from "@/components/repositories/code-scanning-dashboard";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { codeScanningViewState } from "@/lib/code-scanning";
import { getCodeScanningAlerts, getSARIFUploads } from "@/lib/lorehub-api";

type SecurityPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function SecurityPage({ params }: SecurityPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, uploads, alerts] = await Promise.all([
    getDictionary(locale),
    getSARIFUploads(owner, repository),
    getCodeScanningAlerts(owner, repository),
  ]);
  const state = codeScanningViewState(uploads, alerts);

  return (
    <RepositorySection description={dictionary.securityPage.description} title={dictionary.securityPage.title}>
      {state === "forbidden" ? (
        <EmptyState
          body={dictionary.securityPage.forbiddenBody}
          icon={<LockKeyhole aria-hidden="true" />}
          title={dictionary.securityPage.forbiddenTitle}
          tone="warning"
        />
      ) : !uploads.ok || !alerts.ok ? (
        <EmptyState
          body={dictionary.securityPage.unavailableBody}
          icon={<ServerOff aria-hidden="true" />}
          title={dictionary.securityPage.unavailableTitle}
          tone="warning"
        />
      ) : state === "empty" ? (
        <EmptyState
          body={dictionary.securityPage.emptyBody}
          icon={<ShieldCheck aria-hidden="true" />}
          title={dictionary.securityPage.emptyTitle}
        />
      ) : (
        <CodeScanningDashboard alerts={alerts.data} dictionary={dictionary} locale={locale} uploads={uploads.data} />
      )}
    </RepositorySection>
  );
}
