"use client";

import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { RepositoryWebhook, WebhookEvent, WebhookInput } from "@/lib/webhook-client";

import styles from "./repository-webhook-settings.module.css";

type RepositoryWebhookFormProps = {
  availableEvents: WebhookEvent[];
  copy: Dictionary["webhookSettings"];
  editing: RepositoryWebhook | null;
  pending: boolean;
  onCancel: () => void;
  onSubmit: (input: WebhookInput & { secret?: string }) => void;
};

export function RepositoryWebhookForm(props: RepositoryWebhookFormProps) {
  const initial = initialValues(props.editing, props.availableEvents);
  const editing = props.editing !== null;
  const [url, setURL] = useState(initial.url);
  const [secret, setSecret] = useState("");
  const [events, setEvents] = useState<WebhookEvent[]>(initial.events);
  const [active, setActive] = useState(initial.active);
  const valid = validInput(url, events, secret, editing);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!valid) return;
    props.onSubmit({ url: url.trim(), events, active, secret });
  }

  function toggleEvent(value: WebhookEvent) {
    setEvents((current) =>
      current.includes(value) ? current.filter((event) => event !== value) : [...current, value],
    );
  }

  return (
    <form className={styles.form} onSubmit={submit}>
      <h3>{formTitle(props.copy, editing)}</h3>
      <label className={styles.field}>
        <span>{props.copy.payloadUrl}</span>
        <input
          autoCapitalize="none"
          disabled={props.pending}
          maxLength={2048}
          onChange={(event) => setURL(event.target.value)}
          placeholder={props.copy.payloadUrlPlaceholder}
          required
          spellCheck={false}
          type="url"
          value={url}
        />
      </label>
      <label className={styles.field}>
        <span>{props.copy.secret}</span>
        <input
          autoComplete="new-password"
          disabled={props.pending}
          maxLength={512}
          minLength={editing ? undefined : 16}
          onChange={(event) => setSecret(event.target.value)}
          required={!editing}
          type="password"
          value={secret}
        />
        <small>{secretHelp(props.copy, editing)}</small>
      </label>
      <fieldset className={styles.eventFieldset}>
        <legend>{props.copy.events}</legend>
        <div className={styles.eventGrid}>
          {props.availableEvents.map((event) => (
            <label key={event}>
              <input
                checked={events.includes(event)}
                disabled={props.pending}
                onChange={() => toggleEvent(event)}
                type="checkbox"
              />
              <span>{props.copy.eventLabels[event]}</span>
            </label>
          ))}
        </div>
      </fieldset>
      <label className={styles.activeField}>
        <input
          checked={active}
          disabled={props.pending}
          onChange={(event) => setActive(event.target.checked)}
          type="checkbox"
        />
        <span>{props.copy.active}</span>
      </label>
      <div className={styles.formActions}>
        <button className={styles.primaryButton} disabled={props.pending || !valid} type="submit">
          {submitLabel(props.copy, props.pending, editing)}
        </button>
        {editing && (
          <button className={styles.secondaryButton} disabled={props.pending} onClick={props.onCancel} type="button">
            {props.copy.cancel}
          </button>
        )}
      </div>
    </form>
  );
}

function initialValues(editing: RepositoryWebhook | null, availableEvents: WebhookEvent[]) {
  if (editing) return { url: editing.url, events: editing.events, active: editing.active };
  return { url: "", events: availableEvents, active: true };
}

function validInput(url: string, events: WebhookEvent[], secret: string, editing: boolean): boolean {
  return Boolean(url.trim()) && events.length > 0 && (editing || secret.length >= 16);
}

function formTitle(copy: Dictionary["webhookSettings"], editing: boolean): string {
  return editing ? copy.editTitle : copy.createTitle;
}

function secretHelp(copy: Dictionary["webhookSettings"], editing: boolean): string {
  return editing ? copy.secretUpdateHelp : copy.secretCreateHelp;
}

function submitLabel(copy: Dictionary["webhookSettings"], pending: boolean, editing: boolean): string {
  if (pending) return copy.saving;
  return editing ? copy.save : copy.add;
}
