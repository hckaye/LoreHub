import { CheckCircle2, CircleX, Clock3, LoaderCircle, PlayCircle } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { CIRun } from "@/lib/api-types";
import { shortRevision } from "@/lib/format";

import { EmptyState } from "../ui/empty-state";
import { StatusBadge } from "../ui/status-badge";
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
        title={dictionary.actionsPage.noRunsTitle}
        body={dictionary.actionsPage.noRunsBody}
      />
    );
  }

  return (
    <div className={styles.list}>
      {runs.map((run) => (
        <div className={styles.row} key={run.id}>
          <StatusIcon conclusion={run.conclusion} status={run.status} />
          <div>
            <strong>{dictionary.actionsPage.runNumber.replace("{number}", String(run.runNumber))}</strong>
            <p>
              {dictionary.repository.event}: {run.eventName} · {dictionary.common.branch}: {run.branch}
            </p>
          </div>
          <code title={run.revision}>{shortRevision(run.revision)}</code>
          <StatusBadge tone={statusTone(run)}>{statusLabel(run, dictionary)}</StatusBadge>
        </div>
      ))}
    </div>
  );
}

function statusLabel(run: CIRun, dictionary: Dictionary): string {
  const key = run.conclusion ?? run.status;
  return (
    dictionary.actionsPage.statuses[key as keyof typeof dictionary.actionsPage.statuses] ??
    dictionary.actionsPage.statuses.unknown
  );
}

function statusTone(run: CIRun): "neutral" | "success" | "warning" | "danger" {
  if (run.conclusion === "success") {
    return "success";
  }
  if (run.conclusion && run.conclusion !== "skipped") {
    return "danger";
  }
  if (run.status === "queued" || run.status === "in_progress") {
    return "warning";
  }
  return "neutral";
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
