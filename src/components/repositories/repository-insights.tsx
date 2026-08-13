import { BarChart3, Users } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { RepositoryInsightDay, RepositoryInsights } from "@/lib/api-types";
import { formatDateTime } from "@/lib/format";
import { repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import styles from "./repository-insights.module.css";
import { RepositoryPanel } from "./repository-section";

type RepositoryInsightsViewProps = {
  data: RepositoryInsights;
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
};

const periods = [7, 30, 90] as const;

export function RepositoryInsightsView(props: RepositoryInsightsViewProps) {
  const copy = props.dictionary.insightsPage;
  const number = new Intl.NumberFormat(props.locale);
  const activeDays = props.data.activity.filter((day) => activityCount(day) > 0);
  return (
    <div className={styles.stack}>
      <nav aria-label={copy.periodLabel} className={styles.periods}>
        {periods.map((days) => (
          <Link
            aria-current={props.data.periodDays === days ? "page" : undefined}
            href={`${repositoryPath(props.locale, props.owner, props.repository, "insights")}?days=${days}`}
            key={days}
          >
            {periodName(copy.periods, days)}
          </Link>
        ))}
      </nav>

      <RepositoryPanel description={copy.currentDescription} title={copy.currentTitle}>
        <dl className={styles.metrics}>
          <Metric label={copy.openIssues} value={number.format(props.data.current.openIssues)} />
          <Metric label={copy.openPullRequests} value={number.format(props.data.current.openPullRequests)} />
          <Metric label={copy.branches} value={number.format(props.data.current.branches)} />
          <Metric label={copy.publishedReleases} value={number.format(props.data.current.publishedReleases)} />
        </dl>
      </RepositoryPanel>

      <RepositoryPanel
        description={formatPeriodDescription(copy.periodDescription, props.data, props.locale)}
        title={copy.periodTitle}
      >
        <dl className={styles.metrics}>
          <Metric label={copy.issuesOpened} value={number.format(props.data.period.issuesOpened)} />
          <Metric label={copy.issuesClosed} value={number.format(props.data.period.issuesClosed)} />
          <Metric label={copy.pullRequestsOpened} value={number.format(props.data.period.pullRequestsOpened)} />
          <Metric label={copy.pullRequestsMerged} value={number.format(props.data.period.pullRequestsMerged)} />
          <Metric label={copy.workflowRunsCompleted} value={number.format(props.data.period.workflowRunsCompleted)} />
          <Metric label={copy.workflowRunsSucceeded} value={number.format(props.data.period.workflowRunsSucceeded)} />
          <Metric label={copy.releasesPublished} value={number.format(props.data.period.releasesPublished)} />
          <Metric label={copy.branchPushes} value={number.format(props.data.period.branchPushes)} />
        </dl>
      </RepositoryPanel>

      <RepositoryPanel description={copy.activityDescription} title={copy.activityTitle}>
        {activeDays.length === 0 ? (
          <EmptyState
            body={copy.emptyActivityBody}
            icon={<BarChart3 aria-hidden="true" />}
            title={copy.emptyActivityTitle}
          />
        ) : (
          <ActivityList copy={copy} days={activeDays} locale={props.locale} />
        )}
      </RepositoryPanel>

      <RepositoryPanel description={copy.contributorsDescription} title={copy.contributorsTitle}>
        {props.data.contributors.length === 0 ? (
          <p className={styles.muted}>{copy.emptyContributors}</p>
        ) : (
          <ol className={styles.contributors}>
            {props.data.contributors.map((contributor) => (
              <li key={`${contributor.id}:${contributor.username}:${contributor.lastActiveAt}`}>
                <span className={styles.avatar} aria-hidden="true">
                  <Users size={16} />
                </span>
                <span>
                  <strong>{contributor.displayName || contributor.username}</strong>
                  <small>@{contributor.username}</small>
                </span>
                <span className={styles.contributorActivity}>
                  {replaceCount(copy.recordedChanges, number.format(contributor.activityCount))}
                  <small>{replaceDate(copy.lastActive, formatDateTime(contributor.lastActiveAt, props.locale))}</small>
                </span>
              </li>
            ))}
          </ol>
        )}
      </RepositoryPanel>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className={styles.metric}>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function ActivityList({
  copy,
  days,
  locale,
}: {
  copy: Dictionary["insightsPage"];
  days: RepositoryInsightDay[];
  locale: Locale;
}) {
  const max = Math.max(...days.map(activityCount));
  const number = new Intl.NumberFormat(locale);
  return (
    <ol className={styles.activity}>
      {days.map((day) => {
        const count = activityCount(day);
        return (
          <li key={day.date}>
            <time dateTime={day.date}>{formatDate(day.date, locale)}</time>
            <progress aria-label={formatDate(day.date, locale)} max={max} value={count} />
            <span>{number.format(count)}</span>
            <small>{activitySummary(copy, day, number)}</small>
          </li>
        );
      })}
    </ol>
  );
}

function activitySummary(
  copy: Dictionary["insightsPage"],
  day: RepositoryInsightDay,
  number: Intl.NumberFormat,
): string {
  const values: Array<[number, string]> = [
    [day.issuesOpened, copy.issuesOpened],
    [day.issuesClosed, copy.issuesClosed],
    [day.pullRequestsOpened, copy.pullRequestsOpened],
    [day.pullRequestsMerged, copy.pullRequestsMerged],
    [day.workflowRunsCompleted, copy.workflowRunsCompleted],
    [day.releasesPublished, copy.releasesPublished],
    [day.branchPushes, copy.branchPushes],
  ];
  return values
    .filter(([count]) => count > 0)
    .map(([count, label]) => `${label}: ${number.format(count)}`)
    .join(", ");
}

function activityCount(day: RepositoryInsightDay): number {
  return (
    day.issuesOpened +
    day.issuesClosed +
    day.pullRequestsOpened +
    day.pullRequestsMerged +
    day.workflowRunsCompleted +
    day.releasesPublished +
    day.branchPushes
  );
}

function periodName(copy: Dictionary["insightsPage"]["periods"], days: (typeof periods)[number]): string {
  if (days === 7) return copy.seven;
  if (days === 30) return copy.thirty;
  return copy.ninety;
}

function formatPeriodDescription(template: string, data: RepositoryInsights, locale: Locale): string {
  return template
    .replace("{start}", formatDate(data.periodStart, locale))
    .replace("{end}", formatDate(data.periodEnd, locale));
}

function formatDate(value: string, locale: Locale): string {
  const date = new Date(value.includes("T") ? value : `${value}T00:00:00Z`);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeZone: "UTC" }).format(date);
}

function replaceCount(template: string, count: string): string {
  return template.replace("{count}", count);
}

function replaceDate(template: string, date: string): string {
  return template.replace("{date}", date);
}
