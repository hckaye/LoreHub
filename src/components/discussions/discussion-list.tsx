"use client";

import { CircleHelp, Megaphone, MessageCircle, MessageSquare, Search } from "lucide-react";
import Link from "next/link";

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
  const copy = props.dictionary.discussionsPage;
  const allCount =
    props.categories.length > 0
      ? props.categories.reduce((total, category) => total + category.discussionCount, 0)
      : props.page.totalCount;
  return (
    <div className={styles.layout}>
      <aside className={styles.sidebar}>
        <section className={styles.categoryBox}>
          <h2>{copy.categoriesHeading}</h2>
          <nav aria-label={copy.categoriesHeading} className={styles.categoryList}>
            <Link
              aria-current={!props.query.category ? "page" : undefined}
              className={!props.query.category ? styles.categorySelected : undefined}
              href={filterHref(basePath, props.query, undefined)}
            >
              <MessageSquare aria-hidden="true" size={16} />
              <span>{copy.all}</span>
              <strong>{allCount}</strong>
            </Link>
            {props.categories.map((category) => {
              const Icon = categoryIcon(category.format);
              const selected = props.query.category === category.slug;
              return (
                <Link
                  aria-current={selected ? "page" : undefined}
                  className={selected ? styles.categorySelected : undefined}
                  href={filterHref(basePath, props.query, category.slug)}
                  key={category.id}
                >
                  <Icon aria-hidden="true" size={16} />
                  <span>{category.name}</span>
                  <strong>{category.discussionCount}</strong>
                </Link>
              );
            })}
          </nav>
        </section>
      </aside>
      <div className={styles.main}>
        <form action={basePath} className={styles.toolbar} method="get" role="search">
          <input name="category" type="hidden" value={props.query.category ?? ""} />
          <div className={styles.search}>
            <Search aria-hidden="true" size={16} />
            <label className={styles.srOnly} htmlFor="discussion-search">
              {copy.searchPlaceholder}
            </label>
            <input
              defaultValue={props.query.q ?? ""}
              id="discussion-search"
              name="q"
              placeholder={copy.searchPlaceholder}
              type="search"
            />
          </div>
          <label className={styles.selectControl}>
            <span>{copy.state}</span>
            <select
              defaultValue={props.query.state ?? "open"}
              name="state"
              onChange={(event) => event.currentTarget.form?.requestSubmit()}
            >
              <option value="open">{copy.open}</option>
              <option value="closed">{copy.closed}</option>
              <option value="all">{copy.all}</option>
            </select>
          </label>
          <label className={styles.selectControl}>
            <span>{copy.sort}</span>
            <select
              defaultValue={props.query.sort ?? "newest"}
              name="sort"
              onChange={(event) => event.currentTarget.form?.requestSubmit()}
            >
              <option value="newest">{copy.newest}</option>
              <option value="oldest">{copy.oldest}</option>
              <option value="most-commented">{copy.mostCommented}</option>
              <option value="most-voted">{copy.mostVoted}</option>
            </select>
          </label>
          {props.page.viewerCanCreate && (
            <Link className={styles.primaryButton} href={`${basePath}/new`}>
              {copy.newDiscussion}
            </Link>
          )}
        </form>
        {props.page.discussions.length === 0 ? (
          <div className={styles.blankSlate}>
            <MessageCircle aria-hidden="true" size={32} />
            <h2>{copy.noDiscussions}</h2>
            <p>{copy.noDiscussionsDescription}</p>
          </div>
        ) : (
          <section aria-label={copy.title} className={styles.list}>
            {props.page.discussions.map((discussion) => (
              <DiscussionListItem
                dictionary={props.dictionary}
                discussion={discussion}
                href={`${basePath}/${discussion.number}`}
                key={discussion.id}
                locale={props.locale}
              />
            ))}
          </section>
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
      </div>
    </div>
  );
}

function categoryIcon(format: DiscussionCategory["format"]) {
  if (format === "question") return CircleHelp;
  if (format === "announcement") return Megaphone;
  return MessageSquare;
}

function filterHref(basePath: string, query: DiscussionQuery, category: string | undefined): string {
  const params = new URLSearchParams();
  if (query.q) params.set("q", query.q);
  if (category) params.set("category", category);
  if (query.state) params.set("state", query.state);
  if (query.sort) params.set("sort", query.sort);
  const search = params.toString();
  return search ? `${basePath}?${search}` : basePath;
}
