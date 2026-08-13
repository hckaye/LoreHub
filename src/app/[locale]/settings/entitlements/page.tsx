import { ServerOff, ShieldAlert } from "lucide-react";

import { EntitlementSettings } from "@/components/admin/entitlement-settings";
import { AuthRequired } from "@/components/auth/auth-required";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getEntitlements } from "@/lib/lorehub-api";

type EntitlementSettingsPageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function EntitlementSettingsPage({ params }: EntitlementSettingsPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.entitlementSettings;
  if (session.status !== "authenticated") {
    return (
      <RepositorySection description={copy.description} title={copy.title}>
        <AuthRequired dictionary={dictionary} returnTo={`/${locale}/settings/entitlements`} session={session} />
      </RepositorySection>
    );
  }
  const entitlements = await getEntitlements();
  if (!entitlements.ok && (entitlements.reason === "forbidden" || entitlements.reason === "not-found")) {
    return (
      <RepositorySection description={copy.description} title={copy.title}>
        <EmptyState
          body={copy.forbiddenBody}
          icon={<ShieldAlert aria-hidden="true" />}
          title={copy.forbiddenTitle}
          tone="warning"
        />
      </RepositorySection>
    );
  }
  return (
    <RepositorySection description={copy.description} title={copy.title}>
      <RepositoryPanel title={copy.listTitle}>
        {entitlements.ok ? (
          <EntitlementSettings
            dictionary={dictionary}
            initialEntitlements={entitlements.data}
            locale={locale}
            session={session}
          />
        ) : (
          <EmptyState
            body={copy.unavailableBody}
            icon={<ServerOff aria-hidden="true" />}
            title={copy.unavailableTitle}
            tone="warning"
          />
        )}
      </RepositoryPanel>
    </RepositorySection>
  );
}
