import Link from "next/link";
import type { ReactNode } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import { repositoryPath } from "@/lib/routes";
import { parseCodeSearchQualifier, type CodeSearchFile, type CodeSearchResults, type SearchQuery } from "@/lib/search";

import styles from "./search-page.module.css";

type SearchCodeResultsProps = {
  code: CodeSearchResults;
  dictionary: Dictionary;
  locale: Locale;
  query: SearchQuery;
};

export function SearchCodeResults({ code, dictionary, locale, query }: SearchCodeResultsProps) {
  const copy = dictionary.searchPage;
  const qualifier = parseCodeSearchQualifier(query.q);
  if (!qualifier) return null;
  return (
    <section className={styles.section}>
      <h2>
        {copy.code}
        <span>{code.files.length}</span>
      </h2>
      {code.files.length > 0 ? (
        <div className={styles.codeFiles}>
          {code.files.map((file) => (
            <CodeSearchFileResult
              file={file}
              key={file.path}
              locale={locale}
              owner={qualifier.owner}
              queryTerms={qualifier.terms}
              repository={qualifier.repository}
              revision={code.revision}
              dictionary={dictionary}
            />
          ))}
        </div>
      ) : (
        <p className={styles.sectionEmpty}>{copy.codeEmptyBody}</p>
      )}
      {code.truncated && <p className={styles.codeTruncated}>{copy.codeTruncated}</p>}
    </section>
  );
}

function CodeSearchFileResult({
  dictionary,
  file,
  locale,
  owner,
  queryTerms,
  repository,
  revision,
}: {
  dictionary: Dictionary;
  file: CodeSearchFile;
  locale: Locale;
  owner: string;
  queryTerms: string[];
  repository: string;
  revision: string;
}) {
  const copy = dictionary.searchPage;
  const params = new URLSearchParams({ revision, path: file.path });
  const href = `${repositoryPath(locale, owner, repository)}/blob?${params.toString()}`;
  return (
    <article className={styles.codeFile}>
      <header className={styles.codeFileHeader}>
        <h3>
          <Link href={href}>{file.path}</Link>
        </h3>
        <span>{copy.codeMatchCount.replace("{count}", String(file.matchCount))}</span>
      </header>
      <div className={styles.codeMatches}>
        {file.matches.map((match) => (
          <div className={styles.codeMatch} key={`${file.path}:${match.lineNumber}`}>
            <span
              aria-label={copy.codeLine.replace("{line}", String(match.lineNumber))}
              className={styles.codeLineNumber}
            >
              {match.lineNumber}
            </span>
            <code className={styles.codeSnippet}>{highlightSnippet(match.snippet, queryTerms)}</code>
          </div>
        ))}
      </div>
    </article>
  );
}

function highlightSnippet(snippet: string, terms: string[]): ReactNode[] {
  const pattern = terms.map(escapeRegExp).filter(Boolean).join("|");
  if (!pattern) return [snippet];
  const parts = snippet.split(new RegExp(`(${pattern})`, "giu"));
  const matchPattern = new RegExp(`^(?:${pattern})$`, "iu");
  return parts.map((part, index) =>
    matchPattern.test(part) ? (
      <mark key={`${part}-${index}`}>{part}</mark>
    ) : (
      <span key={`${part}-${index}`}>{part}</span>
    ),
  );
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
