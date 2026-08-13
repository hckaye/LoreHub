import { LockKeyhole, ServerOff } from "lucide-react";

import { AuthRequired } from "@/components/auth/auth-required";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { RunnerSettings } from "@/components/runners/runner-settings";
import { OrganizationSettingsTabs, organizationSettingsPath } from "@/components/settings/settings-tabs";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getRunners } from "@/lib/lorehub-api";

type OrganizationRunnerSettingsPageProps = {
  params: Promise<{ locale: string; organization: string }>;
};

export const dynamic = "force-dynamic";

export default async function OrganizationRunnerSettingsPage({ params }: OrganizationRunnerSettingsPageProps) {
  const { locale: value, organization } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.runnerSettings;
  if (session.status !== "authenticated") {
    return (
      <RepositorySection description={copy.organizationDescription} title={copy.title}>
        <AuthRequired
          dictionary={dictionary}
          returnTo={`${organizationSettingsPath(locale, organization)}/runners`}
          session={session}
        />
      </RepositorySection>
    );
  }
  const target = { kind: "organization", organization } as const;
  const runners = await getRunners(target);
  return (
    <RepositorySection description={copy.organizationDescription} title={copy.title}>
      <OrganizationSettingsTabs active="runners" dictionary={dictionary} locale={locale} organization={organization} />
      <RepositoryPanel title={copy.listTitle}>
        {runners.ok ? (
          <RunnerSettings
            dictionary={dictionary}
            initialRunners={runners.data}
            locale={locale}
            session={session}
            target={target}
          />
        ) : runners.reason === "forbidden" || runners.reason === "unauthorized" || runners.reason === "not-found" ? (
          <EmptyState
            body={copy.forbidden}
            icon={<LockKeyhole aria-hidden="true" />}
            title={dictionary.errors.forbidden}
            tone="warning"
          />
        ) : (
          <EmptyState
            body={dictionary.home.apiUnavailableBody}
            icon={<ServerOff aria-hidden="true" />}
            title={copy.unavailable}
            tone="warning"
          />
        )}
      </RepositoryPanel>
    </RepositorySection>
  );
}
