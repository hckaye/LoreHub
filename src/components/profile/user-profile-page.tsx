import { Building2, Globe2, MapPin, UserRound } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository, UserProfile } from "@/lib/api-types";
import { repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import styles from "./user-profile-page.module.css";

type UserProfilePageProps = {
  dictionary: Dictionary;
  locale: Locale;
  profile: UserProfile | null;
  repositories: Repository[] | null;
};

export function UserProfilePage({ dictionary, locale, profile, repositories }: UserProfilePageProps) {
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
  return (
    <div className={styles.page}>
      <section className={styles.hero}>
        <div className={styles.avatar} aria-hidden="true">
          {profile.displayName.slice(0, 1).toUpperCase()}
        </div>
        <div>
          <p className={styles.eyebrow}>{dictionary.profile.title}</p>
          <h1>{profile.displayName}</h1>
          <p className={styles.username}>@{profile.username}</p>
          {profile.bio && <p className={styles.bio}>{profile.bio}</p>}
          <div className={styles.meta}>
            {profile.company && (
              <span>
                <Building2 aria-hidden="true" size={15} />
                {profile.company}
              </span>
            )}
            {profile.location && (
              <span>
                <MapPin aria-hidden="true" size={15} />
                {profile.location}
              </span>
            )}
            {profile.websiteUrl && (
              <a href={profile.websiteUrl} rel="noreferrer" target="_blank">
                <Globe2 aria-hidden="true" size={15} />
                {dictionary.profile.website}
              </a>
            )}
          </div>
        </div>
      </section>
      <div className={styles.content}>
        <section className={styles.panel}>
          <div className={styles.panelHeading}>
            <h2>{dictionary.profile.repositories}</h2>
            <span>{profile.repositoryCount}</span>
          </div>
          {repositories && repositories.length > 0 ? (
            <ul className={styles.repositoryList}>
              {repositories.map((repository) => (
                <li key={repository.id}>
                  <Link href={repositoryPath(locale, repository.owner, repository.slug)}>
                    <strong>{repository.displayName}</strong>
                    <span>
                      {repository.owner}/{repository.slug} · {repository.visibility}
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          ) : (
            <EmptyState
              body={dictionary.home.availablePublicRepositoriesEmptyBody}
              icon={<UserRound aria-hidden="true" />}
              title={dictionary.home.availablePublicRepositoriesEmptyTitle}
            />
          )}
        </section>
      </div>
    </div>
  );
}
