"use client";

import { Pencil, RotateCcw, Trash2 } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { RepositoryWebhook, WebhookDelivery } from "@/lib/webhook-client";

import styles from "./repository-webhook-settings.module.css";

type RepositoryWebhookListProps = {
  copy: Dictionary["webhookSettings"];
  deliveries: Record<string, WebhookDelivery[]>;
  locale: string;
  pendingAction: string;
  webhooks: RepositoryWebhook[];
  onDelete: (webhook: RepositoryWebhook) => void;
  onEdit: (webhook: RepositoryWebhook) => void;
  onRedeliver: (webhook: RepositoryWebhook, delivery: WebhookDelivery) => void;
};

export function RepositoryWebhookList(props: RepositoryWebhookListProps) {
  if (props.webhooks.length === 0) {
    return (
      <div className={styles.empty}>
        <h3>{props.copy.emptyTitle}</h3>
        <p>{props.copy.emptyBody}</p>
      </div>
    );
  }

  return (
    <div className={styles.webhookList}>
      {props.webhooks.map((webhook) => (
        <section className={styles.webhookCard} key={webhook.id}>
          <div className={styles.webhookHeading}>
            <div>
              <strong>{webhook.url}</strong>
              <span className={webhook.active ? styles.activeStatus : styles.inactiveStatus}>
                {webhook.active ? props.copy.enabled : props.copy.disabled}
              </span>
            </div>
            <div className={styles.rowActions}>
              <button
                aria-label={`${props.copy.edit} ${webhook.url}`}
                className={styles.iconButton}
                disabled={Boolean(props.pendingAction)}
                onClick={() => props.onEdit(webhook)}
                title={props.copy.edit}
                type="button"
              >
                <Pencil aria-hidden="true" size={15} />
              </button>
              <button
                aria-label={`${props.copy.delete} ${webhook.url}`}
                className={styles.dangerIconButton}
                disabled={Boolean(props.pendingAction)}
                onClick={() => props.onDelete(webhook)}
                title={props.copy.delete}
                type="button"
              >
                <Trash2 aria-hidden="true" size={15} />
              </button>
            </div>
          </div>
          <p className={styles.events}>{webhook.events.map((event) => props.copy.eventLabels[event]).join(", ")}</p>
          <DeliveryTable
            copy={props.copy}
            deliveries={props.deliveries[webhook.id] ?? []}
            locale={props.locale}
            onRedeliver={(delivery) => props.onRedeliver(webhook, delivery)}
            pendingAction={props.pendingAction}
            webhook={webhook}
          />
        </section>
      ))}
    </div>
  );
}

type DeliveryTableProps = {
  copy: Dictionary["webhookSettings"];
  deliveries: WebhookDelivery[];
  locale: string;
  pendingAction: string;
  webhook: RepositoryWebhook;
  onRedeliver: (delivery: WebhookDelivery) => void;
};

function DeliveryTable(props: DeliveryTableProps) {
  return (
    <div className={styles.deliverySection}>
      <h4>{props.copy.deliveries}</h4>
      {props.deliveries.length === 0 ? (
        <p className={styles.muted}>{props.copy.noDeliveries}</p>
      ) : (
        <div className={styles.tableScroll}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>{props.copy.event}</th>
                <th>{props.copy.status}</th>
                <th>{props.copy.response}</th>
                <th>{props.copy.attempts}</th>
                <th>{props.copy.updated}</th>
                <th>
                  <span className={styles.visuallyHidden}>{props.copy.redeliver}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {props.deliveries.map((delivery) => (
                <tr key={delivery.id}>
                  <td>{eventLabel(props.copy, delivery.event)}</td>
                  <td>{props.copy.statuses[delivery.status]}</td>
                  <td>{delivery.responseStatus ?? (delivery.lastError || "-")}</td>
                  <td>{delivery.attemptCount}</td>
                  <td>
                    <time dateTime={delivery.updatedAt}>{formatDate(delivery.updatedAt, props.locale)}</time>
                  </td>
                  <td>
                    {canRedeliver(delivery) && (
                      <button
                        aria-label={`${props.copy.redeliver} ${delivery.event}`}
                        className={styles.iconButton}
                        disabled={Boolean(props.pendingAction)}
                        onClick={() => props.onRedeliver(delivery)}
                        title={props.copy.redeliver}
                        type="button"
                      >
                        <RotateCcw aria-hidden="true" size={15} />
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function eventLabel(copy: Dictionary["webhookSettings"], event: string): string {
  return event in copy.eventLabels ? copy.eventLabels[event as keyof typeof copy.eventLabels] : event;
}

function canRedeliver(delivery: WebhookDelivery): boolean {
  return delivery.status === "succeeded" || delivery.status === "failed" || delivery.status === "exhausted";
}

function formatDate(value: string, locale: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date);
}
