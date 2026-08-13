import { LockKeyhole, ServerOff } from "lucide-react";

import { AuthRequired } from "@/components/auth/auth-required";
import { LoreServerSettings } from "@/components/lore-servers/lore-server-settings";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { OrganizationSettingsTabs, organizationSettingsPath } from "@/components/settings/settings-tabs";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getDefaultLoreServer, getLoreServers } from "@/lib/lorehub-api";

type LoreServerSettingsPageProps = {
  params: Promise<{ locale: string; organization: string }>;
};

export const dynamic = "force-dynamic";

export default async function LoreServerSettingsPage({ params }: LoreServerSettingsPageProps) {
  const { locale: value, organization } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.loreServerSettings;
  if (session.status !== "authenticated") {
    return (
      <RepositorySection description={copy.description} title={copy.title}>
        <AuthRequired
          dictionary={dictionary}
          returnTo={`${organizationSettingsPath(locale, organization)}/lore-servers`}
          session={session}
        />
      </RepositorySection>
    );
  }
  const [servers, defaultServer] = await Promise.all([
    getLoreServers(organization),
    getDefaultLoreServer(organization),
  ]);
  return (
    <RepositorySection description={copy.description} title={copy.title}>
      <OrganizationSettingsTabs
        active="loreServers"
        dictionary={dictionary}
        locale={locale}
        organization={organization}
      />
      <RepositoryPanel title={copy.listTitle}>
        {servers.ok ? (
          <LoreServerSettings
            dictionary={dictionary}
            initialDefaultServerID={defaultServer.ok ? (defaultServer.data?.id ?? "") : ""}
            initialServers={servers.data}
            locale={locale}
            organization={organization}
            session={session}
          />
        ) : servers.reason === "forbidden" || servers.reason === "unauthorized" || servers.reason === "not-found" ? (
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
