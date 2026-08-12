import { CircleDot, GitPullRequest } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { GlobalWorkItem } from "@/lib/global-work-items";

import workItemStyles from "../work-items/global-work-item-list.module.css";
import { GlobalWorkItemRow } from "../work-items/global-work-item-row";
import styles from "./search-page.module.css";

type SearchWorkItemResultsProps = {
  count: number;
  dictionary: Dictionary;
  items: GlobalWorkItem[];
  kind: "issues" | "pulls";
  locale: Locale;
};

export function SearchWorkItemResults(props: SearchWorkItemResultsProps) {
  const title = props.dictionary.searchPage[props.kind];
  return (
    <section className={styles.section}>
      <h2>
        {props.kind === "issues" ? (
          <CircleDot aria-hidden="true" size={18} />
        ) : (
          <GitPullRequest aria-hidden="true" size={18} />
        )}
        {title}
        <span>{props.count}</span>
      </h2>
      {props.items.length ? (
        <div className={`${workItemStyles.list} ${styles.workItemList}`}>
          {props.items.map((item) => (
            <GlobalWorkItemRow dictionary={props.dictionary} item={item} key={item.id} locale={props.locale} />
          ))}
        </div>
      ) : (
        <p className={styles.sectionEmpty}>{props.dictionary.searchPage.sectionEmpty}</p>
      )}
    </section>
  );
}
