import Link from "next/link";

import styles from "./filter-tabs.module.css";

export type FilterTab = {
  href: string;
  label: string;
  active: boolean;
  count?: number;
};

type FilterTabsProps = {
  label: string;
  tabs: FilterTab[];
};

export function FilterTabs({ label, tabs }: FilterTabsProps) {
  return (
    <nav aria-label={label} className={styles.tabs}>
      {tabs.map((tab) => (
        <Link
          aria-current={tab.active ? "page" : undefined}
          className={tab.active ? styles.active : ""}
          href={tab.href}
          key={tab.href}
        >
          {tab.label}
          {typeof tab.count === "number" && <span>{tab.count}</span>}
        </Link>
      ))}
    </nav>
  );
}
