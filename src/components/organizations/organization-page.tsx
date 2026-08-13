import { Building2, Settings2, UsersRound } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, OrganizationView, Repository, Team } from "@/lib/api-types";
import { localizedPath, repositoryPath } from "@/lib/routes";

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
  const settingsHref = localizedPath(locale, "organizations", organization.slug, "settings");
  return (
    <div className={styles.page}>
      <section className={styles.hero}>
        <div className={styles.avatar} aria-hidden="true">
          {organization.displayName.slice(0, 1).toUpperCase()}
        </div>
        <div className={styles.heroBody}>
          <h1>{organization.displayName}</h1>
          <p className={styles.slug}>{organization.slug}</p>
          {organization.description && <p className={styles.description}>{organization.description}</p>}
          <div className={styles.stats}>
            <span>
              <UsersRound aria-hidden="true" size={15} />
              {organization.memberCount} {dictionary.organizationPage.members}
            </span>
            <span>{dictionary.common[organization.visibility]}</span>
            {organization.role && <span>{dictionary.common[organization.role]}</span>}
          </div>
        </div>
        {canManage && session.status === "authenticated" && (
          <Link className={styles.settingsLink} href={settingsHref}>
            <Settings2 aria-hidden="true" size={16} />
            {dictionary.organizationPage.settings}
          </Link>
        )}
      </section>
      <nav className={styles.tabs} aria-label={dictionary.organizationPage.title}>
        <a href="#repositories">{dictionary.organizationPage.repositories}</a>
        <a href="#teams">{dictionary.organizationPage.teams}</a>
      </nav>
      <div className={styles.grid}>
        <section className={styles.panel} id="repositories">
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
                    <div className={styles.repositoryName}>
                      <strong>{repository.displayName}</strong>
                      <span className={styles.visibility}>{dictionary.common[repository.visibility]}</span>
                    </div>
                    <span className={styles.repositoryPath}>
                      {repository.owner}/{repository.slug}
                    </span>
                    {repository.description && (
                      <span className={styles.repositoryDescription}>{repository.description}</span>
                    )}
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
        <section className={styles.panel} id="teams">
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
                  <Link href={localizedPath(locale, "organizations", organization.slug, "teams", team.slug)}>
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
