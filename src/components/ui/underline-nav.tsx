import Link from "next/link";
import type { ReactNode } from "react";

import styles from "./underline-nav.module.css";

export type UnderlineNavItem = {
  href?: string;
  label: string;
  active?: boolean;
  count?: number;
  icon?: ReactNode;
};

type UnderlineNavProps = {
  label: string;
  items: UnderlineNavItem[];
};

export function UnderlineNav({ label, items }: UnderlineNavProps) {
  return (
    <nav aria-label={label} className={styles.nav}>
      {items.map((item) => {
        const className = item.active ? styles.active : undefined;
        const content = (
          <>
            {item.icon}
            {item.label}
            {typeof item.count === "number" && <span className={styles.counter}>{item.count}</span>}
          </>
        );
        if (item.href) {
          return (
            <Link
              aria-current={item.active ? "page" : undefined}
              className={className}
              href={item.href}
              key={item.href}
            >
              {content}
            </Link>
          );
        }
        return (
          <span aria-current={item.active ? "page" : undefined} className={className} key={item.label}>
            {content}
          </span>
        );
      })}
    </nav>
  );
}
