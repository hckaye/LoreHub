import { Building2 } from "lucide-react";
import Link from "next/link";

import { AuthRequired } from "@/components/auth/auth-required";
import { RepositorySection } from "@/components/repositories/repository-section";
import { FlashNotice } from "@/components/ui/flash-notice";
import { UserAvatar } from "@/components/ui/user-avatar";
import { getDictionary, type Dictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import type { OrganizationView } from "@/lib/api-types";
import { getAuthSession } from "@/lib/auth-api";
import { getDashboard } from "@/lib/lorehub-api";

import styles from "./organizations.module.css";

type OrganizationsPageProps = {
  params: Promise<{ locale: string }>;
};

export const dynamic = "force-dynamic";

function membershipLabel(dictionary: Dictionary, organization: OrganizationView): string {
  const labels = {
    owner: dictionary.common.owner,
    maintainer: dictionary.common.maintainer,
    member: dictionary.common.member,
    public: dictionary.common.public,
    private: dictionary.common.private,
    internal: dictionary.common.internal,
  } as const;
  return labels[organization.role || organization.visibility];
}

function NewOrganizationLink({ href, label }: { href: string; label: string }) {
  return (
    <Link className={styles.primaryButton} href={href}>
      {label}
    </Link>
  );
}

export default async function OrganizationsPage({ params }: OrganizationsPageProps) {
  const { locale: value } = await params;
  const locale = isLocale(value) ? value : "en";
  const [dictionary, session] = await Promise.all([getDictionary(locale), getAuthSession()]);
  if (session.status !== "authenticated") {
    return (
      <div className={styles.page}>
        <RepositorySection title={dictionary.common.organizations}>
          <AuthRequired dictionary={dictionary} returnTo={`/${locale}/organizations`} session={session} />
        </RepositorySection>
      </div>
    );
  }
  const dashboard = await getDashboard();
  const copy = dictionary.organizationPage;
  const newHref = `/${locale}/organizations/new`;
  return (
    <div className={styles.page}>
      <div className={styles.subhead}>
        <h2>{dictionary.common.organizations}</h2>
        <NewOrganizationLink href={newHref} label={copy.newOrganization} />
      </div>
      {!dashboard.ok ? (
        <FlashNotice
          body={dictionary.home.apiUnavailableBody}
          title={dictionary.home.apiUnavailableTitle}
          tone="warning"
        />
      ) : dashboard.data.organizations.length === 0 ? (
        <div className={styles.box}>
          <div className={styles.blankslate}>
            <Building2 aria-hidden="true" className={styles.blankslateIcon} size={24} />
            <h3 className={styles.blankslateHeading}>{copy.emptyTitle}</h3>
            <p className={styles.blankslateBody}>{copy.emptyBody}</p>
            <NewOrganizationLink href={newHref} label={copy.newOrganization} />
          </div>
        </div>
      ) : (
        <ul className={styles.box}>
          {dashboard.data.organizations.map((organization) => (
            <li className={styles.row} key={organization.id}>
              <UserAvatar name={organization.displayName || organization.slug} shape="square" size={32} />
              <span className={styles.identity}>
                <Link
                  className={styles.slug}
                  href={`/${locale}/organizations/${encodeURIComponent(organization.slug)}`}
                >
                  {organization.slug}
                </Link>
                <span className={styles.displayName}>{organization.displayName}</span>
              </span>
              <span className={styles.role}>{membershipLabel(dictionary, organization)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
