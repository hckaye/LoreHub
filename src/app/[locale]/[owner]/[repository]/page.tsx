import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { CodeBrowser } from "@/components/repositories/code-browser";
import { RepositoryAbout } from "@/components/repositories/repository-about";
import { RepositoryReadme } from "@/components/repositories/repository-readme";
import { EmptyState } from "@/components/ui/empty-state";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import type { APIResult } from "@/lib/api-types";
import {
  getBranches,
  getLoreFile,
  getLoreTree,
  getPublicRepository,
  getReleases,
  getRepositoryTags,
  getRevision,
} from "@/lib/lorehub-api";
import { findRepositoryReadme } from "@/lib/repository-readme";

import styles from "./repository.module.css";

type RepositoryCodePageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
  searchParams: Promise<{ branch?: string; path?: string; revision?: string }>;
};

export const dynamic = "force-dynamic";

export default async function RepositoryCodePage({ params, searchParams }: RepositoryCodePageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
  const query = await searchParams;
  const [dictionary, repository] = await Promise.all([getDictionary(locale), getPublicRepository(owner, slug)]);
  if (!repository.ok && repository.reason === "not-found") {
    notFound();
  }
  if (!repository.ok) {
    return (
      <EmptyState
        body={dictionary.home.apiUnavailableBody}
        icon={<ServerOff aria-hidden="true" />}
        title={dictionary.repository.unavailable}
        tone="warning"
      />
    );
  }
  const [branches, tags, releases] = await Promise.all([
    getBranches(owner, slug),
    getRepositoryTags(owner, slug),
    getReleases(owner, slug, 1),
  ]);
  const branch = query.branch || repository.data.defaultBranch;
  const tree = await getLoreTree(owner, slug, {
    branch: query.revision ? undefined : branch,
    revision: query.revision,
    path: query.path,
  });
  const { revision, readme } = await loadTreeDetails(owner, slug, tree);
  const revisionData = resultData(revision);
  return (
    <div className={styles.content}>
      <div className={styles.codeColumn}>
        {tree.ok ? (
          <>
            <CodeBrowser
              branches={resultData(branches) ?? []}
              branch={branch}
              currentRevision={query.revision}
              dictionary={dictionary}
              latestCommit={revisionData}
              locale={locale}
              owner={owner}
              parentRevision={revisionData?.parents[0]}
              repository={slug}
              tags={resultData(tags) ?? []}
              tree={tree.data}
            />
            {readme?.ok && (
              <RepositoryReadme
                dictionary={dictionary}
                entries={tree.data.entries}
                file={readme.data}
                locale={locale}
                owner={owner}
                repository={slug}
              />
            )}
          </>
        ) : (
          <EmptyState
            body={dictionary.home.apiUnavailableBody}
            icon={<ServerOff aria-hidden="true" />}
            title={dictionary.codeBrowser.unavailable}
            tone="warning"
          />
        )}
      </div>
      <RepositoryAbout
        dictionary={dictionary}
        locale={locale}
        owner={owner}
        releasePage={releases.ok ? releases.data : undefined}
        repository={repository.data}
        repositorySlug={slug}
      />
    </div>
  );
}

function resultData<T>(result: APIResult<T> | null): T | undefined {
  return result?.ok ? result.data : undefined;
}

async function loadTreeDetails(owner: string, repository: string, tree: Awaited<ReturnType<typeof getLoreTree>>) {
  if (!tree.ok) {
    return { revision: null, readme: null };
  }
  const readmeEntry = findRepositoryReadme(tree.data.entries);
  const [revision, readme] = await Promise.all([
    getRevision(owner, repository, tree.data.revision),
    readmeEntry
      ? getLoreFile(owner, repository, { revision: tree.data.revision, path: readmeEntry.path })
      : Promise.resolve(null),
  ]);
  return { revision, readme };
}
