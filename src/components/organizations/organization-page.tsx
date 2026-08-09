import { Building2, Settings2, UsersRound } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, OrganizationView, Repository, Team } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import styles from "./organization-page.module.css";
import { OrganizationSettingsForm } from "./organization-settings-form";
import { TeamCreateForm } from "./team-create-form";

type OrganizationPageProps = {
  dictionary: Dictionary;
  locale: Locale;
  organization: OrganizationView | null;
  repositories: Repository[] | null;
  teams: Team[] | null;
  session: AuthSession;
};

export function OrganizationPage({
  dictionary,
  locale,
  organization,
  repositories,
  teams,
  session,
}: OrganizationPageProps) {
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
  return (
    <div className={styles.page}>
      <section className={styles.hero}>
        <div className={styles.mark} aria-hidden="true">
          <Building2 size={28} />
        </div>
        <div>
          <p className={styles.eyebrow}>{dictionary.organizationPage.title}</p>
          <h1>{organization.displayName}</h1>
          <p className={styles.slug}>{organization.slug}</p>
          {organization.description && <p className={styles.description}>{organization.description}</p>}
          <div className={styles.stats}>
            <span>
              <UsersRound aria-hidden="true" size={15} />
              {organization.memberCount} {dictionary.organizationPage.members}
            </span>
            <span>{organization.visibility}</span>
            {organization.role && <span>{organization.role}</span>}
          </div>
        </div>
        {canManage && session.status === "authenticated" && (
          <Link className={styles.settingsLink} href={`/${locale}/organizations/${organization.slug}/settings`}>
            <Settings2 aria-hidden="true" size={16} />
            {dictionary.organizationPage.settings}
          </Link>
        )}
      </section>
      <div className={styles.grid}>
        <section className={styles.panel}>
          <div className={styles.panelHeading}>
            <div>
              <h2>{dictionary.organizationPage.repositories}</h2>
              <p>{dictionary.organizationPage.repositoriesDescription}</p>
            </div>
            <span>{organization.repositoryCount}</span>
          </div>
          {repositories && repositories.length > 0 ? (
            <ul className={styles.list}>
              {repositories.map((repository) => (
                <li key={repository.id}>
                  <Link href={repositoryPath(locale, repository.owner, repository.slug)}>
                    <strong>{repository.displayName}</strong>
                    <span>
                      {repository.slug} · {repository.visibility}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState
              body={dictionary.organizationPage.noRepositoriesBody}
              icon={<Building2 aria-hidden="true" />}
              title={dictionary.organizationPage.noRepositories}
            />
          )}
        </section>
        <section className={styles.panel}>
          <div className={styles.panelHeading}>
            <div>
              <h2>{dictionary.organizationPage.teams}</h2>
              <p>{dictionary.organizationPage.teamsDescription}</p>
            </div>
            <span>{organization.teamCount}</span>
          </div>
          {teams && teams.length > 0 ? (
            <ul className={styles.list}>
              {teams.map((team) => (
                <li key={team.id}>
                  <Link href={`/${locale}/organizations/${organization.slug}/teams/${team.slug}`}>
                    <strong>{team.displayName}</strong>
                    <span>
                      @{team.slug} · {team.memberCount} {dictionary.organizationPage.members}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState
              body={dictionary.organizationPage.noTeamsBody}
              icon={<UsersRound aria-hidden="true" />}
              title={dictionary.organizationPage.noTeams}
            />
          )}
        </section>
      </div>
      {canManage && session.status === "authenticated" && (
        <>
          <TeamCreateForm
            dictionary={dictionary}
            locale={locale}
            organizationSlug={organization.slug}
            session={session}
          />
          <OrganizationSettingsForm dictionary={dictionary} organization={organization} session={session} />
        </>
      )}
    </div>
  );
}
