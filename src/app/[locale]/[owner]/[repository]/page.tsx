import { ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { CodeBrowser } from "@/components/repositories/code-browser";
import { RepositoryFacts } from "@/components/repositories/repository-facts";
import { EmptyState } from "@/components/ui/empty-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getBranches, getLoreTree, getPublicRepository, getRevision } from "@/lib/lorehub-api";

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
  const branches = await getBranches(owner, slug);
  const branch = query.branch || repository.data.defaultBranch;
  const tree = await getLoreTree(owner, slug, {
    branch: query.revision ? undefined : branch,
    revision: query.revision,
    path: query.path,
  });
  const revision = tree.ok ? await getRevision(owner, slug, tree.data.revision) : null;
  return (
    <div className={styles.content}>
      <SectionHeading description={dictionary.repository.codeDescription} title={dictionary.repository.codeTitle} />
      <RepositoryFacts dictionary={dictionary} repository={repository.data} />
      {tree.ok ? (
        <CodeBrowser
          branches={branches.ok ? branches.data : []}
          branch={branch}
          dictionary={dictionary}
          locale={locale}
          owner={owner}
          parentRevision={revision?.ok ? revision.data.parents[0] : undefined}
          repository={slug}
          tree={tree.data}
        />
      ) : (
        <EmptyState
          body={dictionary.home.apiUnavailableBody}
          icon={<ServerOff aria-hidden="true" />}
          title={dictionary.codeBrowser.unavailable}
          tone="warning"
        />
      )}
    </div>
  );
}
