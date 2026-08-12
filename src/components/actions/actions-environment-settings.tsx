"use client";

import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { ActionsEnvironment, AuthSession } from "@/lib/api-types";
import { deleteJson, putJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import styles from "./actions-environment-settings.module.css";

type ActionsEnvironmentSettingsProps = {
  dictionary: Dictionary;
  environments: ActionsEnvironment[];
  owner: string;
  repository: string;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

type EnvironmentDraft = {
  name: string;
  waitTimerMinutes: string;
  reviewers: string;
  preventSelfReview: boolean;
};

const emptyDraft: EnvironmentDraft = {
  name: "",
  waitTimerMinutes: "0",
  reviewers: "",
  preventSelfReview: true,
};

export function ActionsEnvironmentSettings({
  dictionary,
  environments: initialEnvironments,
  owner,
  repository,
  session,
}: ActionsEnvironmentSettingsProps) {
  const copy = dictionary.actionsEnvironments;
  const [environments, setEnvironments] = useState(initialEnvironments);
  const [draft, setDraft] = useState<EnvironmentDraft>(emptyDraft);
  const [editingName, setEditingName] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/actions`;

  function edit(environment: ActionsEnvironment) {
    setEditingName(environment.name);
    setDraft({
      name: environment.name,
      waitTimerMinutes: String(environment.waitTimerMinutes),
      reviewers: environment.reviewers.map((reviewer) => reviewer.username).join(", "),
      preventSelfReview: environment.preventSelfReview,
    });
    setError("");
    setNotice("");
  }

  function reset() {
    setEditingName("");
    setDraft(emptyDraft);
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const name = draft.name.trim();
    const waitTimerMinutes = Number(draft.waitTimerMinutes);
    if (!name || !Number.isInteger(waitTimerMinutes)) return;
    setPending(true);
    setError("");
    setNotice("");
    const result = await putJson<ActionsEnvironment>(
      `${base}/environments/${encodeURIComponent(editingName || name)}`,
      {
        waitTimerMinutes,
        preventSelfReview: draft.preventSelfReview,
        reviewers: reviewerNames(draft.reviewers),
      },
      session.csrfToken,
    );
    setPending(false);
    if (!result.ok) {
      setError(mutationFailureMessage(result.kind, dictionary));
      return;
    }
    setEnvironments((current) =>
      [...current.filter((environment) => environment.id !== result.data.id), result.data].sort((left, right) =>
        left.name.localeCompare(right.name),
      ),
    );
    setNotice(copy.saved);
    reset();
  }

  async function remove(environment: ActionsEnvironment) {
    if (!window.confirm(copy.deleteConfirm.replace("{name}", environment.name))) return;
    setPending(true);
    setError("");
    setNotice("");
    const result = await deleteJson(`${base}/environments/${encodeURIComponent(environment.name)}`, session.csrfToken);
    setPending(false);
    if (!result.ok) {
      setError(mutationFailureMessage(result.kind, dictionary));
      return;
    }
    setEnvironments((current) => current.filter((item) => item.id !== environment.id));
    if (editingName === environment.name) reset();
    setNotice(copy.deleted);
  }

  return (
    <div className={styles.stack}>
      <div aria-live="polite">
        {notice && <p className={styles.success}>{notice}</p>}
        {error && <p className={styles.error}>{error}</p>}
      </div>
      {environments.length === 0 ? (
        <p className={styles.empty}>{copy.empty}</p>
      ) : (
        <div className={styles.list}>
          {environments.map((environment) => (
            <div className={styles.environment} key={environment.id}>
              <div>
                <strong>{environment.name}</strong>
                <span>
                  {copy.summary
                    .replace("{reviewers}", String(environment.reviewers.length))
                    .replace("{minutes}", String(environment.waitTimerMinutes))}
                </span>
              </div>
              <div className={styles.actions}>
                <button disabled={pending} onClick={() => edit(environment)} type="button">
                  {copy.edit}
                </button>
                <button disabled={pending} onClick={() => void remove(environment)} type="button">
                  {copy.delete}
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
      <form className={styles.form} onSubmit={(event) => void save(event)}>
        <h3>{editingName ? copy.editTitle : copy.createTitle}</h3>
        <div className={styles.fields}>
          <label>
            {copy.name}
            <input
              disabled={pending || Boolean(editingName)}
              maxLength={128}
              onChange={(event) => setDraft({ ...draft, name: event.target.value })}
              required
              value={draft.name}
            />
          </label>
          <label>
            {copy.waitTimer}
            <input
              disabled={pending}
              max={43200}
              min={0}
              onChange={(event) => setDraft({ ...draft, waitTimerMinutes: event.target.value })}
              required
              type="number"
              value={draft.waitTimerMinutes}
            />
          </label>
        </div>
        <label>
          {copy.reviewers}
          <input
            disabled={pending}
            onChange={(event) => setDraft({ ...draft, reviewers: event.target.value })}
            placeholder={copy.reviewersPlaceholder}
            value={draft.reviewers}
          />
          <span>{copy.reviewersHelp}</span>
        </label>
        <label className={styles.checkbox}>
          <input
            checked={draft.preventSelfReview}
            disabled={pending}
            onChange={(event) => setDraft({ ...draft, preventSelfReview: event.target.checked })}
            type="checkbox"
          />
          {copy.preventSelfReview}
        </label>
        <div className={styles.actions}>
          <button disabled={pending || !draft.name.trim()} type="submit">
            {pending ? copy.saving : copy.save}
          </button>
          {editingName && (
            <button disabled={pending} onClick={reset} type="button">
              {copy.cancel}
            </button>
          )}
        </div>
      </form>
    </div>
  );
}

function reviewerNames(value: string): string[] {
  return value
    .split(",")
    .map((name) => name.trim())
    .filter(Boolean);
}
