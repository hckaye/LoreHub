import { BookMarked, Building2, Settings, UsersRound } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

import { EmptyState } from "@/components/ui/empty-state";
import { UnderlineNav } from "@/components/ui/underline-nav";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, OrganizationView } from "@/lib/api-types";
import { localizedPath } from "@/lib/routes";

import styles from "./organization-profile.module.css";

type OrganizationProfileProps = {
  children?: ReactNode;
  dictionary: Dictionary;
  locale: Locale;
  organization: OrganizationView | null;
  session: AuthSession;
  tab: "repositories" | "teams";
};

export function OrganizationProfile({
  children,
  dictionary,
  locale,
  organization,
  session,
  tab,
}: OrganizationProfileProps) {
  if (!organization) {
    return (
      <div className={styles.page}>
        <EmptyState
          body={dictionary.home.apiUnavailableBody}
          icon={<Building2 aria-hidden="true" />}
          title={dictionary.home.apiUnavailableTitle}
          tone="warning"
        />
      </div>
    );
  }
  const canManage = organization.role === "owner" || organization.role === "maintainer";
  const orgBase = localizedPath(locale, "organizations", organization.slug);
  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div aria-hidden="true" className={styles.avatar}>
          {organization.displayName.slice(0, 1).toUpperCase()}
        </div>
        <div className={styles.identity}>
          <h1>{organization.displayName}</h1>
          <p className={styles.meta}>
            <span>{organization.slug}</span>
            <span>
              {organization.memberCount} {dictionary.organizationPage.members}
            </span>
            <span>{dictionary.common[organization.visibility]}</span>
          </p>
          {organization.description ? <p className={styles.description}>{organization.description}</p> : null}
        </div>
        {canManage && session.status === "authenticated" ? (
          <Link className={styles.settingsButton} href={`${orgBase}/settings`}>
            <Settings aria-hidden="true" size={16} />
            {dictionary.organizationPage.settings}
          </Link>
        ) : null}
      </header>
      <UnderlineNav
        items={[
          {
            active: tab === "repositories",
            count: organization.repositoryCount,
            href: orgBase,
            icon: <BookMarked aria-hidden="true" size={16} />,
            label: dictionary.organizationPage.repositories,
          },
          {
            active: tab === "teams",
            count: organization.teamCount,
            href: `${orgBase}/teams`,
            icon: <UsersRound aria-hidden="true" size={16} />,
            label: dictionary.organizationPage.teams,
          },
        ]}
        label={dictionary.organizationPage.title}
      />
      {children}
    </div>
  );
}
