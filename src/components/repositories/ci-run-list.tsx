import { CheckCircle2, CircleX, Clock3, LoaderCircle, PlayCircle } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { CIRun } from "@/lib/api-types";

import { EmptyState } from "../ui/empty-state";
import styles from "./ci-run-list.module.css";

type CIRunListProps = {
  runs: CIRun[];
  dictionary: Dictionary;
};

export function CIRunList({ runs, dictionary }: CIRunListProps) {
  if (runs.length === 0) {
    return (
      <EmptyState
        icon={<PlayCircle aria-hidden="true" />}
        title={dictionary.repository.noCIRuns}
        body={dictionary.repository.ciRunsDescription}
      />
    );
  }

  return (
    <div className={styles.list}>
      {runs.map((run) => (
        <div className={styles.row} key={run.id}>
          <StatusIcon conclusion={run.conclusion} status={run.status} />
          <div>
            <strong>
              {dictionary.repository.run} #{run.runNumber}
            </strong>
            <p>
              {run.eventName} · {run.branch}
            </p>
          </div>
          <code title={run.revision}>{shortRevision(run.revision)}</code>
          <span className={styles.status}>{run.conclusion ?? run.status}</span>
        </div>
      ))}
    </div>
  );
}

function StatusIcon({ status, conclusion }: Pick<CIRun, "status" | "conclusion">) {
  if (status === "queued") {
    return <Clock3 aria-hidden="true" className={styles.queued} size={18} />;
  }
  if (status === "in_progress") {
    return <LoaderCircle aria-hidden="true" className={styles.running} size={18} />;
  }
  if (conclusion === "success") {
    return <CheckCircle2 aria-hidden="true" className={styles.success} size={18} />;
  }
  return <CircleX aria-hidden="true" className={styles.failure} size={18} />;
}

function shortRevision(revision: string): string {
  return revision.length > 12 ? revision.slice(0, 12) : revision;
}
