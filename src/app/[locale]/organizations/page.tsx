import { Building2 } from "lucide-react";
import Link from "next/link";

import { AuthRequired } from "@/components/auth/auth-required";
import { RepositorySection } from "@/components/repositories/repository-section";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getAuthSession } from "@/lib/auth-api";
import { getDashboard } from "@/lib/lorehub-api";

import styles from "./organizations.module.css";

type OrganizationsPageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

export default async function OrganizationsPage({ params }: OrganizationsPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  if (session.status !== "authenticated") {
    return (
      <RepositorySection description={dictionary.home.dashboardDescription} title={dictionary.common.organizations}>
        <AuthRequired dictionary={dictionary} returnTo={`/${locale}/organizations`} session={session} />
      </RepositorySection>
    );
  }
  const dashboard = await getDashboard();
  return (
    <RepositorySection description={dictionary.home.dashboardDescription} title={dictionary.common.organizations}>
      {dashboard.ok && dashboard.data.organizations.length > 0 ? (
        <ul className={styles.list}>
          {dashboard.data.organizations.map((organization) => (
            <li key={organization.id}>
              <Link href={`/${locale}/organizations/${encodeURIComponent(organization.slug)}`}>
                <span className={styles.icon} aria-hidden="true">
                  <Building2 size={18} />
                </span>
                <span>
                  <strong>{organization.displayName}</strong>
                  <small>
                    {organization.slug} · {organization.role || organization.visibility}
                  </small>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState
          body={dashboard.ok ? dictionary.organizationPage.noOrganizationsBody : dictionary.home.apiUnavailableBody}
          icon={<Building2 aria-hidden="true" />}
          title={dashboard.ok ? dictionary.organizationPage.noOrganizations : dictionary.home.apiUnavailableTitle}
          tone={dashboard.ok ? "neutral" : "warning"}
        />
      )}
    </RepositorySection>
  );
}
