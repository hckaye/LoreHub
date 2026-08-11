"use client";

import { useEffect, useState, type ReactNode } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession } from "@/lib/api-types";
import {
  createRepositoryWebhook,
  deleteRepositoryWebhook,
  listRepositoryWebhooks,
  listWebhookDeliveries,
  redeliverWebhook,
  updateRepositoryWebhook,
  type RepositoryWebhook,
  type WebhookDelivery,
  type WebhookEvent,
  type WebhookFailureKind,
  type WebhookInput,
} from "@/lib/webhook-client";

import { RepositoryWebhookForm } from "./repository-webhook-form";
import { RepositoryWebhookList } from "./repository-webhook-list";
import styles from "./repository-webhook-settings.module.css";

type RepositoryWebhookSettingsProps = {
  dictionary: Dictionary;
  locale: string;
  owner: string;
  repository: string;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

type LoadState = "loading" | "ready" | "forbidden" | "unavailable";

export function RepositoryWebhookSettings(props: RepositoryWebhookSettingsProps) {
  const copy = props.dictionary.webhookSettings;
  const [webhooks, setWebhooks] = useState<RepositoryWebhook[]>([]);
  const [availableEvents, setAvailableEvents] = useState<WebhookEvent[]>([]);
  const [deliveries, setDeliveries] = useState<Record<string, WebhookDelivery[]>>({});
  const [loadState, setLoadState] = useState<LoadState>("loading");
  const [reloadKey, setReloadKey] = useState(0);
  const [formRevision, setFormRevision] = useState(0);
  const [editing, setEditing] = useState<RepositoryWebhook | null>(null);
  const [pendingAction, setPendingAction] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    void loadWebhooks(props.owner, props.repository, controller.signal).then((result) => {
      if (controller.signal.aborted) return;
      if (!result.ok) {
        setLoadState(isPermissionFailure(result.kind) ? "forbidden" : "unavailable");
        return;
      }
      setWebhooks(result.webhooks);
      setAvailableEvents(result.availableEvents);
      setDeliveries(result.deliveries);
      setLoadState("ready");
    });
    return () => controller.abort();
  }, [props.owner, props.repository, reloadKey]);

  async function save(input: WebhookInput & { secret?: string }) {
    setPendingAction(editing?.id ?? "create");
    clearMessages();
    const result = editing
      ? await updateRepositoryWebhook(
          props.owner,
          props.repository,
          editing.id,
          withoutEmptySecret(input),
          props.session.csrfToken,
        )
      : await createRepositoryWebhook(
          props.owner,
          props.repository,
          input as WebhookInput & { secret: string },
          props.session.csrfToken,
        );
    setPendingAction("");
    if (!result.ok) {
      setError(mutationFailureMessage(copy, result.kind));
      return;
    }
    setWebhooks((current) => upsertWebhook(current, result.data));
    setDeliveries((current) => ({ ...current, [result.data.id]: current[result.data.id] ?? [] }));
    setEditing(null);
    setFormRevision((current) => current + 1);
    setNotice(copy.saved);
  }

  async function remove(webhook: RepositoryWebhook) {
    if (!window.confirm(copy.deleteConfirm)) return;
    setPendingAction(webhook.id);
    clearMessages();
    const result = await deleteRepositoryWebhook(props.owner, props.repository, webhook.id, props.session.csrfToken);
    setPendingAction("");
    if (!result.ok) {
      setError(mutationFailureMessage(copy, result.kind));
      return;
    }
    setWebhooks((current) => current.filter((item) => item.id !== webhook.id));
    setDeliveries((current) => withoutKey(current, webhook.id));
    if (editing?.id === webhook.id) setEditing(null);
    setNotice(copy.deleted);
  }

  async function redeliver(webhook: RepositoryWebhook, delivery: WebhookDelivery) {
    const action = `${webhook.id}:${delivery.id}`;
    setPendingAction(action);
    clearMessages();
    const result = await redeliverWebhook(
      props.owner,
      props.repository,
      webhook.id,
      delivery.id,
      props.session.csrfToken,
    );
    setPendingAction("");
    if (!result.ok) {
      setError(mutationFailureMessage(copy, result.kind));
      return;
    }
    setDeliveries((current) => ({
      ...current,
      [webhook.id]: (current[webhook.id] ?? []).map((item) =>
        item.id === delivery.id ? { ...item, status: "queued", lastError: "", responseBody: "" } : item,
      ),
    }));
    setNotice(copy.redeliveryQueued);
  }

  function retryLoad() {
    clearMessages();
    setLoadState("loading");
    setReloadKey((current) => current + 1);
  }

  function clearMessages() {
    setError("");
    setNotice("");
  }

  if (loadState === "loading") return <p className={styles.muted}>{copy.loading}</p>;
  if (loadState === "forbidden") {
    return <WebhookCallout title={copy.forbiddenTitle} body={copy.forbiddenBody} />;
  }
  if (loadState === "unavailable") {
    return (
      <WebhookCallout title={copy.unavailableTitle} body={copy.unavailableBody}>
        <button className={styles.secondaryButton} onClick={retryLoad} type="button">
          {copy.retry}
        </button>
      </WebhookCallout>
    );
  }

  return (
    <div className={styles.stack}>
      <div aria-live="polite" className={styles.statusArea}>
        {notice && (
          <p className={styles.success} role="status">
            {notice}
          </p>
        )}
        {error && (
          <p className={styles.error} role="alert">
            {error}
          </p>
        )}
      </div>
      <RepositoryWebhookList
        copy={copy}
        deliveries={deliveries}
        locale={props.locale}
        onDelete={(webhook) => void remove(webhook)}
        onEdit={(webhook) => {
          clearMessages();
          setEditing(webhook);
        }}
        onRedeliver={(webhook, delivery) => void redeliver(webhook, delivery)}
        pendingAction={pendingAction}
        webhooks={webhooks}
      />
      <RepositoryWebhookForm
        availableEvents={availableEvents}
        copy={copy}
        editing={editing}
        key={editing?.id ?? `new-${formRevision}`}
        onCancel={() => {
          setEditing(null);
          setFormRevision((current) => current + 1);
        }}
        onSubmit={(input) => void save(input)}
        pending={pendingAction !== ""}
      />
    </div>
  );
}

type LoadedWebhooks = {
  ok: true;
  webhooks: RepositoryWebhook[];
  availableEvents: WebhookEvent[];
  deliveries: Record<string, WebhookDelivery[]>;
};

async function loadWebhooks(
  owner: string,
  repository: string,
  signal: AbortSignal,
): Promise<LoadedWebhooks | { ok: false; kind: WebhookFailureKind }> {
  const result = await listRepositoryWebhooks(owner, repository, signal);
  if (!result.ok) return result;
  const deliveryResults = await Promise.all(
    result.data.webhooks.map(async (webhook) => ({
      webhook,
      result: await listWebhookDeliveries(owner, repository, webhook.id, signal),
    })),
  );
  const failure = deliveryResults.find((item) => !item.result.ok);
  if (failure && !failure.result.ok) return failure.result;
  return {
    ok: true,
    webhooks: result.data.webhooks,
    availableEvents: result.data.availableEvents,
    deliveries: Object.fromEntries(
      deliveryResults.map(({ webhook, result: deliveriesResult }) => [
        webhook.id,
        deliveriesResult.ok ? deliveriesResult.data : [],
      ]),
    ),
  };
}

function withoutEmptySecret(input: WebhookInput): WebhookInput {
  const normalized = { ...input };
  if (!normalized.secret) delete normalized.secret;
  return normalized;
}

function upsertWebhook(webhooks: RepositoryWebhook[], webhook: RepositoryWebhook): RepositoryWebhook[] {
  return [...webhooks.filter((item) => item.id !== webhook.id), webhook].sort((left, right) =>
    left.url.localeCompare(right.url),
  );
}

function withoutKey<T>(record: Record<string, T>, key: string): Record<string, T> {
  return Object.fromEntries(Object.entries(record).filter(([entryKey]) => entryKey !== key));
}

function isPermissionFailure(kind: WebhookFailureKind): boolean {
  return kind === "unauthorized" || kind === "forbidden";
}

function mutationFailureMessage(copy: Dictionary["webhookSettings"], kind: WebhookFailureKind): string {
  if (isPermissionFailure(kind)) return copy.mutationForbidden;
  if (kind === "invalid") return copy.invalid;
  if (kind === "conflict") return copy.conflict;
  return copy.mutationUnavailable;
}

function WebhookCallout(props: { title: string; body: string; children?: ReactNode }) {
  return (
    <div className={styles.callout} role="alert">
      <h3>{props.title}</h3>
      <p>{props.body}</p>
      {props.children}
    </div>
  );
}
