import { Clock3, GitBranch, GitCommitHorizontal, ShieldAlert } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { CodeScanningAlert, CodeScanningSeverity, SARIFUploadMetadata } from "@/lib/api-types";
import { codeScanningAlertRows, type CodeScanningAlertRow } from "@/lib/code-scanning";
import { formatTimestamp, shortRevision } from "@/lib/format";

import styles from "./code-scanning-dashboard.module.css";
import { RepositoryPanel } from "./repository-section";

type CodeScanningDashboardProps = {
  alerts: CodeScanningAlert[];
  uploads: SARIFUploadMetadata[];
  dictionary: Dictionary;
  locale: Locale;
};

export function CodeScanningDashboard({ alerts, uploads, dictionary, locale }: CodeScanningDashboardProps) {
  const rows = codeScanningAlertRows(alerts, uploads);
  const summary = (
    <div className={styles.summary}>
      <span>{dictionary.securityPage.alertsShown.replace("{count}", String(alerts.length))}</span>
      <span>{dictionary.securityPage.recentUploads.replace("{count}", String(uploads.length))}</span>
    </div>
  );
  return (
    <RepositoryPanel
      actions={summary}
      description={dictionary.securityPage.codeScanningDescription}
      title={dictionary.securityPage.codeScanningTitle}
    >
      <ol className={styles.alertList}>
        {rows.map((row) => (
          <CodeScanningAlertItem dictionary={dictionary} key={row.alert.id} locale={locale} row={row} />
        ))}
      </ol>
    </RepositoryPanel>
  );
}

function CodeScanningAlertItem({
  dictionary,
  locale,
  row,
}: {
  dictionary: Dictionary;
  locale: Locale;
  row: CodeScanningAlertRow;
}) {
  const { alert, upload } = row;
  const uploadedAt = upload?.createdAt ?? alert.createdAt;
  return (
    <li className={styles.alertRow}>
      <span className={styles.severityIcon} data-severity={alert.level}>
        <ShieldAlert aria-hidden="true" size={19} />
      </span>
      <article className={styles.alertMain}>
        <div className={styles.titleLine}>
          <strong>{alert.message}</strong>
          <span className={styles.severity} data-severity={alert.level}>
            {severityLabel(alert.level, dictionary)}
          </span>
        </div>
        <dl className={styles.findingDetails}>
          <div>
            <dt>{dictionary.securityPage.rule}</dt>
            <dd>
              <code>{alert.ruleId}</code>
            </dd>
          </div>
          <div>
            <dt>{dictionary.securityPage.tool}</dt>
            <dd>{alert.tool}</dd>
          </div>
          <div>
            <dt>{dictionary.securityPage.location}</dt>
            <dd>
              <code>{locationLabel(alert)}</code>
            </dd>
          </div>
        </dl>
        <div className={styles.uploadDetails}>
          {upload ? (
            <>
              <span title={upload.ref}>
                <GitBranch aria-hidden="true" size={14} />
                <span className={styles.detailLabel}>{dictionary.securityPage.ref}</span>
                <code>{upload.ref}</code>
              </span>
              <span title={upload.revision}>
                <GitCommitHorizontal aria-hidden="true" size={14} />
                <span className={styles.detailLabel}>{dictionary.securityPage.revision}</span>
                <code>{shortRevision(upload.revision)}</code>
              </span>
            </>
          ) : (
            <span>{dictionary.securityPage.metadataUnavailable}</span>
          )}
          <span>
            <Clock3 aria-hidden="true" size={14} />
            <span className={styles.detailLabel}>{dictionary.securityPage.uploadedAt}</span>
            <time dateTime={uploadedAt}>{formatTimestamp(uploadedAt, locale)}</time>
          </span>
        </div>
      </article>
    </li>
  );
}

function severityLabel(level: CodeScanningSeverity, dictionary: Dictionary): string {
  return dictionary.securityPage.severities[level];
}

function locationLabel(alert: CodeScanningAlert): string {
  return alert.startLine ? `${alert.path}:${alert.startLine}` : alert.path;
}
