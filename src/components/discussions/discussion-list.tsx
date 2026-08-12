import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { DiscussionCategory, DiscussionPage } from "@/lib/api-types";
import type { DiscussionQuery } from "@/lib/lorehub-api";
import { repositoryPath } from "@/lib/routes";

import { DiscussionListItem } from "./discussion-list-item";
import { DiscussionListPagination } from "./discussion-list-pagination";
import styles from "./discussion-list.module.css";

type DiscussionListProps = {
  categories: DiscussionCategory[];
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
  page: DiscussionPage;
  query: DiscussionQuery;
};

export function DiscussionList(props: DiscussionListProps) {
  const basePath = repositoryPath(props.locale, props.owner, props.repository, "discussions");
  const pages = Math.max(1, Math.ceil(props.page.totalCount / props.page.perPage));
  return (
    <>
      <form className={styles.filters} method="get">
        <div className={styles.search}>
          <input
            defaultValue={props.query.q ?? ""}
            name="q"
            placeholder={props.dictionary.discussionsPage.searchPlaceholder}
          />
          <button type="submit">{props.dictionary.discussionsPage.search}</button>
        </div>
        <div className={styles.selects}>
          <label>
            {props.dictionary.discussionsPage.category}
            <select defaultValue={props.query.category ?? ""} name="category">
              <option value="">{props.dictionary.discussionsPage.allCategories}</option>
              {props.categories.map((category) => (
                <option key={category.id} value={category.slug}>
                  {category.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            {props.dictionary.discussionsPage.state}
            <select defaultValue={props.query.state ?? "open"} name="state">
              <option value="open">{props.dictionary.discussionsPage.open}</option>
              <option value="closed">{props.dictionary.discussionsPage.closed}</option>
              <option value="all">{props.dictionary.discussionsPage.all}</option>
            </select>
          </label>
          <label>
            {props.dictionary.discussionsPage.sort}
            <select defaultValue={props.query.sort ?? "newest"} name="sort">
              <option value="newest">{props.dictionary.discussionsPage.newest}</option>
              <option value="oldest">{props.dictionary.discussionsPage.oldest}</option>
              <option value="most-commented">{props.dictionary.discussionsPage.mostCommented}</option>
              <option value="most-voted">{props.dictionary.discussionsPage.mostVoted}</option>
            </select>
          </label>
        </div>
      </form>
      {props.page.discussions.length === 0 ? (
        <div className={styles.empty}>
          <h2>{props.dictionary.discussionsPage.noDiscussions}</h2>
          <p>{props.dictionary.discussionsPage.noDiscussionsDescription}</p>
        </div>
      ) : (
        <div className={styles.list}>
          {props.page.discussions.map((discussion) => (
            <DiscussionListItem
              dictionary={props.dictionary}
              discussion={discussion}
              href={`${basePath}/${discussion.number}`}
              key={discussion.id}
              locale={props.locale}
            />
          ))}
        </div>
      )}
      {pages > 1 && (
        <DiscussionListPagination
          basePath={basePath}
          dictionary={props.dictionary}
          page={props.page.page}
          pages={pages}
          query={props.query}
        />
      )}
    </>
  );
}
