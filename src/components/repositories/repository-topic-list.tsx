import Link from "next/link";

import type { Locale } from "@/i18n/config";

import styles from "./repository-topic-list.module.css";

type RepositoryTopicListProps = {
  className?: string;
  label: string;
  limit?: number;
  locale: Locale;
  topics: string[];
};

export function RepositoryTopicList({ className, label, limit, locale, topics }: RepositoryTopicListProps) {
  const visibleTopics = limit === undefined ? topics : topics.slice(0, limit);
  if (visibleTopics.length === 0) return null;
  const classes = className ? `${styles.list} ${className}` : styles.list;
  return (
    <nav aria-label={label} className={classes}>
      {visibleTopics.map((topic) => (
        <Link href={`/${locale}/search?q=${encodeURIComponent(topic)}&type=repositories`} key={topic}>
          {topic}
        </Link>
      ))}
    </nav>
  );
}
