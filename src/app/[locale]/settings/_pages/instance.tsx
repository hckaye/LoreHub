import { ServerOff, ShieldAlert } from "lucide-react";

import { AccountSettingsHeader } from "@/components/account/account-settings-shell";
import { InstanceSettings } from "@/components/admin/instance-settings";
import { AuthRequired } from "@/components/auth/auth-required";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getAdminSettings } from "@/lib/lorehub-api";
import { accountSettingsPath } from "@/lib/routes";

type InstanceSettingsPageProps = {
  params: Promise<{ locale: string }>;
};

export default async function InstanceSettingsPage({ params }: InstanceSettingsPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  const copy = dictionary.instanceSettings;
  if (session.status !== "authenticated") {
    return (
      <AuthRequired dictionary={dictionary} returnTo={accountSettingsPath(locale, "instance")} session={session} />
    );
  }
  const settings = await getAdminSettings();
  if (!settings.ok && (settings.reason === "forbidden" || settings.reason === "not-found")) {
    return (
      <>
        <AccountSettingsHeader title={copy.title} />
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
      <AccountSettingsHeader title={copy.title} />
      {settings.ok ? (
        <InstanceSettings dictionary={dictionary} initialSettings={settings.data} session={session} />
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
