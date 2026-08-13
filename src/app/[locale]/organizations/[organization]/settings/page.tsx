import { ServerOff } from "lucide-react";

import { ActionsContextSettings } from "@/components/actions/actions-context-settings";
import { AuthRequired } from "@/components/auth/auth-required";
import { DeletedRepositorySettings } from "@/components/organizations/deleted-repository-settings";
import { OrganizationTeamSettings } from "@/components/organizations/organization-team-settings";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { OrganizationSettingsTabs } from "@/components/settings/settings-tabs";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getDeletedRepositories, getOrganization } from "@/lib/lorehub-api";

type OrganizationSettingsPageProps = {
  params: Promise<{ locale: string; organization: string }>;
};

export const dynamic = "force-dynamic";

export default async function OrganizationSettingsPage({ params }: OrganizationSettingsPageProps) {
  const { locale: value, organization } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session, organizationResult] = await Promise.all([
    getDictionary(locale),
    getAuthSession(),
    getOrganization(organization),
  ]);
  if (session.status !== "authenticated") {
    return (
      <RepositorySection
        description={dictionary.organizationSettingsPage.description}
        title={dictionary.organizationSettingsPage.title}
      >
        <AuthRequired
          dictionary={dictionary}
          returnTo={`/${locale}/organizations/${encodeURIComponent(organization)}/settings`}
          session={session}
        />
      </RepositorySection>
    );
  }
  if (session.user === null) {
    return (
      <EmptyState
        body={dictionary.auth.requiredBody}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.auth.requiredTitle}
        tone="warning"
      />
    );
  }
  const deletedResult =
    organizationResult.ok && organizationResult.data.role === "owner"
      ? await getDeletedRepositories(organization)
      : null;
  return (
    <RepositorySection
      description={dictionary.organizationSettingsPage.description}
      title={dictionary.organizationSettingsPage.title}
    >
      <OrganizationSettingsTabs active="general" dictionary={dictionary} locale={locale} organization={organization} />
      <RepositoryPanel
        description={dictionary.actionsSettings.organizationDescription}
        title={dictionary.actionsSettings.title}
      >
        <ActionsContextSettings
          dictionary={dictionary}
          locale={locale}
          session={session}
          target={{ kind: "organization", organization }}
        />
      </RepositoryPanel>
      <OrganizationTeamSettings dictionary={dictionary} organization={organization} session={session} />
      {deletedResult && (
        <RepositoryPanel
          description={dictionary.repositoryLifecycle.deletedRepositoriesDescription}
          title={dictionary.repositoryLifecycle.deletedRepositoriesTitle}
        >
          {deletedResult.ok ? (
            <DeletedRepositorySettings
              dictionary={dictionary}
              locale={locale}
              repositories={deletedResult.data}
              session={session}
            />
          ) : (
            <EmptyState
              body={dictionary.home.apiUnavailableBody}
              icon={<ServerOff aria-hidden="true" />}
              title={dictionary.home.apiUnavailableTitle}
              tone="warning"
            />
          )}
        </RepositoryPanel>
      )}
    </RepositorySection>
  );
}
