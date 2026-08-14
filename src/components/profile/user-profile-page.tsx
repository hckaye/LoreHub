import { BookMarked, Building2, Calendar, Link2, MapPin, UserRound } from "lucide-react";

import { RepositoryRows } from "@/components/repositories/repository-rows";
import { UnderlineNav } from "@/components/ui/underline-nav";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository, UserProfile } from "@/lib/api-types";
import { formatDate } from "@/lib/format";

import { EmptyState } from "../ui/empty-state";
import { UserAvatar } from "../ui/user-avatar";
import styles from "./user-profile-page.module.css";

type UserProfilePageProps = {
  dictionary: Dictionary;
  locale: Locale;
  profile: UserProfile | null;
  repositories: Repository[] | null;
  unavailable?: boolean;
};

export function UserProfilePage({ dictionary, locale, profile, repositories, unavailable }: UserProfilePageProps) {
  if (!profile) {
    return (
      <div className={styles.page}>
        <EmptyState
          body={dictionary.home.apiUnavailableBody}
          icon={<UserRound aria-hidden="true" />}
          title={dictionary.home.apiUnavailableTitle}
          tone="warning"
        />
      </div>
    );
  }
  const website = profile.websiteUrl ? displayWebsite(profile.websiteUrl) : "";
  return (
    <div className={styles.page}>
      <aside className={styles.rail}>
        <div className={styles.avatarWrap}>
          <UserAvatar avatarUrl={profile.avatarUrl} name={profile.displayName} size={296} />
        </div>
        <div className={styles.names}>
          <h1 className={styles.displayName}>{profile.displayName}</h1>
          <p className={styles.username}>{profile.username}</p>
          {profile.pronouns ? <p className={styles.pronouns}>{profile.pronouns}</p> : null}
        </div>
        {profile.bio ? <p className={styles.bio}>{profile.bio}</p> : null}
        <ul className={styles.meta}>
          {profile.company ? (
            <li>
              <Building2 aria-hidden="true" size={16} />
              {profile.company}
            </li>
          ) : null}
          {profile.location ? (
            <li>
              <MapPin aria-hidden="true" size={16} />
              {profile.location}
            </li>
          ) : null}
          {website ? (
            <li>
              <a href={profile.websiteUrl} rel="noreferrer" target="_blank">
                <Link2 aria-hidden="true" size={16} />
                {website}
              </a>
            </li>
          ) : null}
          <li>
            <Calendar aria-hidden="true" size={16} />
            {dictionary.profile.joined} {formatDate(profile.createdAt, locale)}
          </li>
        </ul>
      </aside>
      <div className={styles.content}>
        <UnderlineNav
          items={[
            {
              active: true,
              count: profile.repositoryCount,
              icon: <BookMarked aria-hidden="true" size={16} />,
              label: dictionary.profile.repositories,
            },
          ]}
          label={dictionary.profile.title}
        />
        <RepositoryRows
          dictionary={dictionary}
          locale={locale}
          repositories={repositories}
          unavailable={Boolean(unavailable)}
        />
      </div>
    </div>
  );
}

function displayWebsite(url: string): string {
  return url.replace(/^https?:\/\//, "").replace(/\/$/, "");
}
