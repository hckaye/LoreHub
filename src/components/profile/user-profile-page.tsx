"use client";

import { Building2, Globe2, MapPin, Search, UserRound } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { Repository, UserProfile } from "@/lib/api-types";
import { formatDate } from "@/lib/format";
import { repositoryPath } from "@/lib/routes";

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

export function UserProfilePage({
  dictionary,
  locale,
  profile,
  repositories,
  unavailable,
}: UserProfilePageProps) {
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
      <aside className={styles.rail}>
        <UserAvatar avatarUrl={profile.avatarUrl} name={profile.displayName} size={200} />
        <h1 className={styles.displayName}>{profile.displayName}</h1>
        <p className={styles.username}>{profile.username}</p>
        {profile.pronouns && <p className={styles.pronouns}>{profile.pronouns}</p>}
        {profile.bio && <p className={styles.bio}>{profile.bio}</p>}
        <ul className={styles.meta}>
          {profile.company && (
            <li>
              <Building2 aria-hidden="true" size={15} />
              {profile.company}
            </li>
          )}
          {profile.location && (
            <li>
              <MapPin aria-hidden="true" size={15} />
              {profile.location}
            </li>
          )}
          {profile.websiteUrl && (
            <li>
              <a href={profile.websiteUrl} rel="noreferrer" target="_blank">
                <Globe2 aria-hidden="true" size={15} />
                {dictionary.profile.website}
              </a>
            </li>
          )}
          <li>
            <UserRound aria-hidden="true" size={15} />
            {dictionary.profile.joined} {formatDate(profile.createdAt, locale)}
          </li>
        </ul>
      </aside>
      <div className={styles.content}>
        <ProfileRepositories
          dictionary={dictionary}
          locale={locale}
          repositories={repositories}
          totalCount={profile.repositoryCount}
          unavailable={Boolean(unavailable)}
        />
      </div>
    </div>
  );
}

type ProfileRepositoriesProps = {
  dictionary: Dictionary;
  locale: Locale;
  repositories: Repository[] | null;
  totalCount: number;
  unavailable: boolean;
};

function ProfileRepositories({ dictionary, locale, repositories, totalCount, unavailable }: ProfileRepositoriesProps) {
  const [query, setQuery] = useState("");
  const filtered = useMemo(() => {
    const list = repositories ?? [];
    const normalized = query.trim().toLocaleLowerCase();
    if (!normalized) {
      return list;
    }
    return list.filter((repository) =>
      [repository.owner, repository.slug, repository.displayName, repository.description, ...repository.topics]
        .join(" ")
        .toLocaleLowerCase()
        .includes(normalized),
    );
  }, [repositories, query]);

  return (
    <section className={styles.panel}>
      <div className={styles.panelHeading}>
        <h2>{dictionary.profile.repositories}</h2>
        <span>{totalCount}</span>
      </div>
      <div className={styles.filter}>
        <Search aria-hidden="true" size={15} />
        <input
          aria-label={dictionary.profile.filterRepositories}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={dictionary.profile.filterRepositories}
          type="search"
          value={query}
        />
      </div>
      {filtered.length > 0 ? (
        <ul className={styles.repositoryList}>
          {filtered.map((repository) => (
            <li key={repository.id}>
              <Link href={repositoryPath(locale, repository.owner, repository.slug)}>
                <div className={styles.repositoryName}>
                  <strong>{repository.displayName}</strong>
                  <span className={styles.visibility}>{dictionary.common[repository.visibility]}</span>
                </div>
                <span className={styles.repositoryPath}>
                  {repository.owner}/{repository.slug}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState
          body={
            unavailable
              ? dictionary.home.apiUnavailableBody
              : query.trim()
                ? dictionary.home.searchEmptyBody
                : dictionary.profile.repositoriesEmptyBody
          }
          icon={<UserRound aria-hidden="true" />}
          title={
            unavailable
              ? dictionary.home.apiUnavailableTitle
              : query.trim()
                ? dictionary.home.searchEmptyTitle
                : dictionary.profile.repositoriesEmptyTitle
          }
          tone={unavailable ? "warning" : "neutral"}
        />
      )}
    </section>
  );
}
