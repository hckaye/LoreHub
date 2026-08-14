import { UsersRound } from "lucide-react";
import Link from "next/link";

import { EmptyState } from "@/components/ui/empty-state";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, OrganizationView, Team } from "@/lib/api-types";
import { localizedPath } from "@/lib/routes";

import { OrganizationProfile } from "./organization-profile";
import styles from "./organization-profile.module.css";
import { TeamCreateForm } from "./team-create-form";

type OrganizationTeamsPageProps = {
  dictionary: Dictionary;
  locale: Locale;
  organization: OrganizationView | null;
  session: AuthSession;
  teams: Team[] | null;
};

export function OrganizationTeamsPage({
  dictionary,
  locale,
  organization,
  session,
  teams,
}: OrganizationTeamsPageProps) {
  if (!organization) {
    return (
      <OrganizationProfile dictionary={dictionary} locale={locale} organization={null} session={session} tab="teams" />
    );
  }
  const canManage = organization.role === "owner" || organization.role === "maintainer";
  return (
    <OrganizationProfile
      dictionary={dictionary}
      locale={locale}
      organization={organization}
      session={session}
      tab="teams"
    >
      {teams && teams.length > 0 ? (
        <ul className={styles.teamList}>
          {teams.map((team) => (
            <li className={styles.teamRow} key={team.id}>
              <h3>
                <Link href={localizedPath(locale, "organizations", organization.slug, "teams", team.slug)}>
                  {team.displayName}
                </Link>
              </h3>
              <p className={styles.teamMeta}>
                @{team.slug} · {team.memberCount} {dictionary.organizationPage.members}
              </p>
              {team.description ? <p className={styles.teamDescription}>{team.description}</p> : null}
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
      {canManage && session.status === "authenticated" ? (
        <TeamCreateForm
          dictionary={dictionary}
          locale={locale}
          organizationSlug={organization.slug}
          session={session}
        />
      ) : null}
    </OrganizationProfile>
  );
}
