import { ServerOff } from "lucide-react";
import Link from "next/link";

import { ActionsContextSettings } from "@/components/actions/actions-context-settings";
import { AuthRequired } from "@/components/auth/auth-required";
import { OrganizationTeamSettings } from "@/components/organizations/organization-team-settings";
import { RepositoryPanel, RepositorySection } from "@/components/repositories/repository-section";
import sectionStyles from "@/components/repositories/repository-section.module.css";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getOrganization } from "@/lib/lorehub-api";

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
  return (
    <RepositorySection
      actions={
        organizationResult.ok && organizationResult.data.role === "owner" ? (
          <Link
            className={sectionStyles.secondaryButton}
            href={`/${locale}/organizations/${encodeURIComponent(organization)}/settings/audit-log`}
          >
            {dictionary.auditLog.open}
          </Link>
        ) : undefined
      }
      description={dictionary.organizationSettingsPage.description}
      title={dictionary.organizationSettingsPage.title}
    >
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
    </RepositorySection>
  );
}
