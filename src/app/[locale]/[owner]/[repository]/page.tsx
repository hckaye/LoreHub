import { FileCode2, ServerOff } from "lucide-react";
import { notFound } from "next/navigation";

import { BranchList } from "@/components/repositories/branch-list";
import { BranchSelector } from "@/components/repositories/branch-selector";
import { RepositoryFacts } from "@/components/repositories/repository-facts";
import { EmptyState } from "@/components/ui/empty-state";
import { SectionHeading } from "@/components/ui/section-heading";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";
import { getBranches, getPublicRepository } from "@/lib/lorehub-api";

import styles from "./repository.module.css";

type RepositoryCodePageProps = {
  params: Promise<{ locale: string; owner: string; repository: string }>;
};

export const dynamic = "force-dynamic";

export default async function RepositoryCodePage({ params }: RepositoryCodePageProps) {
  const { locale: value, owner, repository: slug } = await params;
  const locale = isLocale(value) ? value : "en";
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
  const latestBranch = branches.ok
    ? (branches.data.find((branch) => branch.name === repository.data.defaultBranch) ??
      branches.data.find((branch) => branch.current))
    : null;
  return (
    <div className={styles.content}>
      <SectionHeading description={dictionary.repository.codeDescription} title={dictionary.repository.codeTitle} />
      <RepositoryFacts dictionary={dictionary} repository={repository.data} />
      <div className={styles.overviewGrid}>
        <section aria-labelledby="summary-title" className={styles.panel}>
          <div className={styles.panelHeading}>
            <div>
              <h2 id="summary-title">{dictionary.repository.repositorySummary}</h2>
              <p>{dictionary.repository.readOnlyNote}</p>
            </div>
          </div>
          <dl className={styles.details}>
            <div>
              <dt>{dictionary.repository.sourceRepository}</dt>
              <dd>
                <code>{repository.data.loreUrl}</code>
              </dd>
            </div>
            <div>
              <dt>{dictionary.repository.defaultBranch}</dt>
              <dd>
                <code>{repository.data.defaultBranch}</code>
              </dd>
            </div>
            <div>
              <dt>{dictionary.repository.latestRevision}</dt>
              <dd>
                <code>{latestBranch?.latestRevision ?? dictionary.insightsPage.metricUnavailable}</code>
              </dd>
            </div>
          </dl>
        </section>
        {branches.ok && (
          <BranchSelector
            branches={branches.data}
            defaultBranch={repository.data.defaultBranch}
            dictionary={dictionary}
          />
        )}
      </div>
      <section aria-labelledby="branches-title" className={styles.panel}>
        <div className={styles.panelHeading}>
          <div>
            <h2 id="branches-title">{dictionary.repository.branchesTitle}</h2>
            <p>{dictionary.repository.branchesDescription}</p>
          </div>
        </div>
        {branches.ok ? (
          <BranchList branches={branches.data} dictionary={dictionary} />
        ) : (
          <EmptyState
            body={dictionary.repository.branchesDescription}
            icon={<ServerOff aria-hidden="true" />}
            title={dictionary.repository.unavailable}
            tone="warning"
          />
        )}
      </section>
      <section className={styles.panel}>
        <EmptyState
          body={dictionary.repository.fileTreeDescription}
          icon={<FileCode2 aria-hidden="true" />}
          title={dictionary.repository.noFileTree}
        />
      </section>
    </div>
  );
}
