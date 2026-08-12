import { BookOpenText } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { LoreFile, TreeEntry } from "@/lib/api-types";
import { createRepositoryReadmeURLTransform } from "@/lib/repository-readme";
import { repositoryPath } from "@/lib/routes";

import { MarkdownContent } from "../wiki/markdown-content";
import styles from "./repository-readme.module.css";

type RepositoryReadmeProps = {
  dictionary: Dictionary;
  entries: TreeEntry[];
  file: LoreFile;
  locale: Locale;
  owner: string;
  repository: string;
};

export function RepositoryReadme(props: RepositoryReadmeProps) {
  const copy = props.dictionary.codeBrowser;
  const base = repositoryPath(props.locale, props.owner, props.repository);
  const query = new URLSearchParams({ revision: props.file.revision, path: props.file.path }).toString();
  const filePath = `${base}/blob?${query}`;
  const rawPath = `/api/v1/repositories/${encodeURIComponent(props.owner)}/${encodeURIComponent(
    props.repository,
  )}/raw?${query}`;
  const urlTransform = createRepositoryReadmeURLTransform({
    locale: props.locale,
    owner: props.owner,
    repository: props.repository,
    revision: props.file.revision,
    readmePath: props.file.path,
    entries: props.entries,
  });
  return (
    <section aria-labelledby="repository-readme-title" className={styles.panel}>
      <header className={styles.header}>
        <h2 id="repository-readme-title">
          <BookOpenText aria-hidden="true" size={17} />
          <Link href={filePath}>{props.file.path}</Link>
        </h2>
        <a href={rawPath}>{copy.raw}</a>
      </header>
      <div className={styles.content}>
        {props.file.truncated ? (
          <p className={styles.status}>{copy.tooLarge}</p>
        ) : props.file.binary ? (
          <p className={styles.status}>{copy.binary}</p>
        ) : props.file.content ? (
          <MarkdownContent body={props.file.content} urlTransform={urlTransform} />
        ) : (
          <p className={styles.status}>{copy.emptyReadme}</p>
        )}
      </div>
    </section>
  );
}
