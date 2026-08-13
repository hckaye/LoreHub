import { LockKeyhole, ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { AuthRequired } from "@/components/auth/auth-required";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { RunnerSettings } from "@/components/runners/runner-settings";
import { RepositorySettingsTabs } from "@/components/settings/settings-tabs";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getRunners } from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

type RepositoryRunnerSettingsPageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function RepositoryRunnerSettingsPage({ params }: RepositoryRunnerSettingsPageProps) {
  const { locale: value, owner, repository } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.runnerSettings;
  if (session.status !== "authenticated") {
    return (
      <RepositorySection description={copy.repositoryDescription} title={copy.title}>
        <AuthRequired
          dictionary={dictionary}
          returnTo={`${repositoryPath(locale, owner, repository, "settings")}/runners`}
          session={session}
        />
      </RepositorySection>
    );
  }
  const target = { kind: "repository", owner, repository } as const;
  const runners = await getRunners(target);
  if (!runners.ok && runners.reason === "not-found") notFound();
  return (
    <RepositorySection description={copy.repositoryDescription} title={copy.title}>
      <RepositorySettingsTabs
        active="runners"
        dictionary={dictionary}
        locale={locale}
        owner={owner}
        repository={repository}
      />
      <RepositoryPanel title={copy.listTitle}>
        {runners.ok ? (
          <RunnerSettings
            dictionary={dictionary}
            initialRunners={runners.data}
            locale={locale}
            session={session}
            target={target}
          />
        ) : runners.reason === "forbidden" || runners.reason === "unauthorized" ? (
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
