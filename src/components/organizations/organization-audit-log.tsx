import { History, Search } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuditEvent, AuditLogPage } from "@/lib/api-types";

import { EmptyState } from "../ui/empty-state";
import styles from "./organization-audit-log.module.css";

type OrganizationAuditLogProps = {
  data: AuditLogPage;
  dictionary: Dictionary;
  locale: Locale;
  organization: string;
  query: string;
};

export function OrganizationAuditLog(props: OrganizationAuditLogProps) {
  const copy = props.dictionary.auditLog;
  const basePath = `/${props.locale}/organizations/${encodeURIComponent(props.organization)}/settings/audit-log`;
  return (
    <div className={styles.stack}>
      <form action={basePath} className={styles.filters} method="get" role="search">
        <label htmlFor="audit-log-query">{copy.searchLabel}</label>
        <div className={styles.searchRow}>
          <span className={styles.searchInput}>
            <Search aria-hidden="true" size={16} />
            <input
              defaultValue={props.query}
              id="audit-log-query"
              maxLength={200}
              name="query"
              placeholder={copy.searchPlaceholder}
              type="search"
            />
          </span>
          <button type="submit">{copy.search}</button>
          {props.query && <Link href={basePath}>{copy.clear}</Link>}
        </div>
      </form>
      {props.data.items.length === 0 ? (
        <EmptyState body={copy.emptyBody} icon={<History aria-hidden="true" />} title={copy.emptyTitle} />
      ) : (
        <ol className={styles.events}>
          {props.data.items.map((event) => (
            <AuditEventRow dictionary={props.dictionary} event={event} key={event.id} locale={props.locale} />
          ))}
        </ol>
      )}
      {props.data.nextCursor && (
        <nav aria-label={copy.title} className={styles.pagination}>
          <Link href={auditPagePath(basePath, props.query, props.data.nextCursor)}>{copy.next}</Link>
        </nav>
      )}
    </div>
  );
}

function AuditEventRow({ dictionary, event, locale }: { dictionary: Dictionary; event: AuditEvent; locale: Locale }) {
  const copy = dictionary.auditLog;
  const details = Object.entries(event.details).sort(([left], [right]) => left.localeCompare(right));
  return (
    <li className={styles.event}>
      <div className={styles.eventHeading}>
        <code>{event.action}</code>
        <time dateTime={event.occurredAt}>{formatDate(event.occurredAt, locale)}</time>
      </div>
      <dl className={styles.summary}>
        <AuditField label={copy.actor} value={actorName(event, copy.system)} />
        <AuditField label={copy.repository} value={repositoryName(event, copy.noRepository)} />
        <AuditField label={copy.target} value={targetName(event)} />
        {event.remoteAddress && <AuditField label={copy.sourceAddress} value={event.remoteAddress} />}
      </dl>
      {details.length > 0 && (
        <details className={styles.details}>
          <summary>{copy.details}</summary>
          <dl>
            {details.map(([key, value]) => (
              <AuditField key={key} label={displayDetailKey(key)} value={displayDetailValue(value)} />
            ))}
          </dl>
        </details>
      )}
    </li>
  );
}

function AuditField({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function actorName(event: AuditEvent, fallback: string): string {
  if (!event.actor) return fallback;
  return event.actor.displayName ? `${event.actor.displayName} (@${event.actor.username})` : `@${event.actor.username}`;
}

function repositoryName(event: AuditEvent, fallback: string): string {
  return event.repository ? `${event.repository.owner}/${event.repository.slug}` : fallback;
}

function targetName(event: AuditEvent): string {
  return event.targetId ? `${event.targetType} · ${event.targetId}` : event.targetType;
}

function displayDetailKey(value: string): string {
  return value.replaceAll("_", " ");
}

function displayDetailValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === null) return "null";
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value) ?? String(value);
}

function formatDate(value: string, locale: Locale): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "medium" }).format(date);
}

function auditPagePath(basePath: string, query: string, before: string): string {
  const search = new URLSearchParams({ before });
  if (query) search.set("query", query);
  return `${basePath}?${search.toString()}`;
}
