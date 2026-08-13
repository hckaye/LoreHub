import { History, LockKeyhole, ServerOff } from "lucide-react";

import { AuthRequired } from "@/components/auth/auth-required";
import { OrganizationAuditLog } from "@/components/organizations/organization-audit-log";
import { RepositorySection } from "@/components/repositories/repository-section";
import { OrganizationSettingsTabs } from "@/components/settings/settings-tabs";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getOrganizationAuditLog } from "@/lib/lorehub-api";

type AuditLogPageProps = {
  params: Promise<{ locale: string; organization: string }>;
  searchParams: Promise<{ before?: string | string[]; query?: string | string[] }>;
};

export const dynamic = "force-dynamic";

export default async function AuditLogPage({ params, searchParams }: AuditLogPageProps) {
  const { locale: value, organization } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session, search] = await Promise.all([getDictionary(locale), getAuthSession(), searchParams]);
  const copy = dictionary.auditLog;
  const settingsPath = `/${locale}/organizations/${encodeURIComponent(organization)}/settings`;
  if (session.status !== "authenticated") {
    return (
      <RepositorySection description={copy.description} title={copy.title}>
        <AuthRequired dictionary={dictionary} returnTo={`${settingsPath}/audit-log`} session={session} />
      </RepositorySection>
    );
  }
  const query = singleValue(search.query).trim();
  const before = singleValue(search.before);
  const result = await getOrganizationAuditLog(organization, query, before);
  return (
    <RepositorySection description={copy.description} title={copy.title}>
      <OrganizationSettingsTabs active="auditLog" dictionary={dictionary} locale={locale} organization={organization} />
      {result.ok ? (
        <OrganizationAuditLog
          data={result.data}
          dictionary={dictionary}
          locale={locale}
          organization={organization}
          query={query}
        />
      ) : result.reason === "forbidden" || result.reason === "not-found" ? (
        <EmptyState
          body={copy.forbiddenBody}
          icon={<LockKeyhole aria-hidden="true" />}
          title={copy.forbiddenTitle}
          tone="warning"
        />
      ) : result.reason === "invalid" ? (
        <EmptyState
          body={result.code === "invalid_cursor" ? copy.invalidCursor : copy.invalidSearch}
          icon={<History aria-hidden="true" />}
          title={copy.unavailableTitle}
          tone="warning"
        />
      ) : (
        <EmptyState
          body={copy.unavailableBody}
          icon={<ServerOff aria-hidden="true" />}
          title={copy.unavailableTitle}
          tone="warning"
        />
      )}
    </RepositorySection>
  );
}

function singleValue(value: string | string[] | undefined): string {
  return typeof value === "string" ? value : "";
}
