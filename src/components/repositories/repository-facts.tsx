import { CircleDot, GitBranch, GitPullRequest, PlayCircle } from "lucide-react";
import type { ReactNode } from "react";

import type { Dictionary } from "@/i18n";
import type { Repository } from "@/lib/api-types";

import styles from "./repository-facts.module.css";

type RepositoryFactsProps = {
  repository: Repository;
  dictionary: Dictionary;
  openIssues?: number | null;
  openPullRequests?: number | null;
  actionsRuns?: number | null;
};

export function RepositoryFacts({
  repository,
  dictionary,
  openIssues,
  openPullRequests,
  actionsRuns,
}: RepositoryFactsProps) {
  const issueLabel = typeof openIssues === "undefined" ? dictionary.common.issues : dictionary.insightsPage.openIssues;
  const issueValue =
    typeof openIssues === "undefined"
      ? repository.issueCount
      : (openIssues ?? dictionary.insightsPage.metricUnavailable);
  const pullRequestLabel =
    typeof openPullRequests === "undefined" ? dictionary.common.pullRequests : dictionary.insightsPage.openPullRequests;
  const pullRequestValue =
    typeof openPullRequests === "undefined"
      ? repository.mergeRequestCount
      : (openPullRequests ?? dictionary.insightsPage.metricUnavailable);
  return (
    <div className={styles.grid}>
      <Fact icon={<CircleDot aria-hidden="true" size={17} />} label={issueLabel} value={issueValue} />
      <Fact icon={<GitPullRequest aria-hidden="true" size={17} />} label={pullRequestLabel} value={pullRequestValue} />
      {typeof actionsRuns === "number" && (
        <Fact
          icon={<PlayCircle aria-hidden="true" size={17} />}
          label={dictionary.insightsPage.actionsRuns}
          value={actionsRuns}
        />
      )}
      <Fact
        icon={<GitBranch aria-hidden="true" size={17} />}
        label={dictionary.common.defaultBranch}
        value={repository.defaultBranch}
      />
    </div>
  );
}

function Fact({ icon, label, value }: { icon: ReactNode; label: string; value: number | string }) {
  return (
    <div className={styles.fact}>
      <span className={styles.icon}>{icon}</span>
      <span className={styles.label}>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
