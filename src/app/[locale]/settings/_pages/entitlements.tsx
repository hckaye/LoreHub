import { ServerOff, ShieldAlert } from "lucide-react";

import { AccountSettingsHeader } from "@/components/account/account-settings-shell";
import { EntitlementSettings } from "@/components/admin/entitlement-settings";
import { AuthRequired } from "@/components/auth/auth-required";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getEntitlements } from "@/lib/lorehub-api";
import { accountSettingsPath } from "@/lib/routes";

type EntitlementSettingsPageProps = {
  params: Promise<{ locale: string }>;
};

export default async function EntitlementSettingsPage({ params }: EntitlementSettingsPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.entitlementSettings;
  if (session.status !== "authenticated") {
    return (
      <AuthRequired dictionary={dictionary} returnTo={accountSettingsPath(locale, "entitlements")} session={session} />
    );
  }
  const entitlements = await getEntitlements();
  if (!entitlements.ok && (entitlements.reason === "forbidden" || entitlements.reason === "not-found")) {
    return (
      <>
        <AccountSettingsHeader description={copy.description} title={copy.title} />
        <EmptyState
          body={copy.forbiddenBody}
          icon={<ShieldAlert aria-hidden="true" />}
          title={copy.forbiddenTitle}
          tone="warning"
        />
      </>
    );
  }
  return (
    <>
      <AccountSettingsHeader description={copy.description} title={copy.title} />
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
    </>
  );
}
