import { UserRound } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { SearchUser } from "@/lib/search";

import styles from "./search-page.module.css";

type SearchUserResultsProps = {
  count: number;
  dictionary: Dictionary;
  locale: Locale;
  users: SearchUser[];
};

export function SearchUserResults(props: SearchUserResultsProps) {
  return (
    <section className={styles.section}>
      <h2>
        <UserRound aria-hidden="true" size={18} />
        {props.dictionary.searchPage.users}
        <span>{props.count}</span>
      </h2>
      {props.users.length ? (
        <ul className={styles.list}>
          {props.users.map((user) => (
            <li key={user.id}>
              <Link href={`/${props.locale}/${encodeURIComponent(user.username)}`}>
                <strong>{user.displayName}</strong>
                <span>@{user.username}</span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <p className={styles.sectionEmpty}>{props.dictionary.searchPage.sectionEmpty}</p>
      )}
    </section>
  );
}
