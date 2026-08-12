"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import {
  actionsContextEntryPath,
  actionsContextListPath,
  deleteActionsContextEntry,
  listActionsContextEntries,
  putActionsContextEntry,
  type ActionsContextEntry,
  type ActionsContextFailureKind,
  type ActionsContextLocation,
} from "@/lib/actions-context-client";
import type { AuthSession } from "@/lib/api-types";

import { ActionsContextEntryForm, type ActionsContextValueKind } from "./actions-context-entry-form";
import { ActionsContextEntryList } from "./actions-context-entry-list";
import styles from "./actions-context-settings.module.css";

type ActionsContextTarget =
  { kind: "organization"; organization: string } | { kind: "repository"; owner: string; repository: string };

type ActionsContextSettingsProps = {
  dictionary: Dictionary;
  locale: string;
  session: Extract<AuthSession, { status: "authenticated" }>;
  target: ActionsContextTarget;
  environmentNames?: string[];
};

type LoadState = "loading" | "ready" | "environment-required" | "forbidden" | "unavailable";

export function ActionsContextSettings({
  dictionary,
  environmentNames = [],
  locale,
  session,
  target,
}: ActionsContextSettingsProps) {
  const copy = dictionary.actionsSettings;
  const [scopeKind, setScopeKind] = useState<"repository" | "environment">("repository");
  const [environmentDraft, setEnvironmentDraft] = useState("");
  const [environment, setEnvironment] = useState("");
  const [entries, setEntries] = useState<ActionsContextEntry[]>([]);
  const [loadState, setLoadState] = useState<LoadState>("loading");
  const [reloadKey, setReloadKey] = useState(0);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [valueKind, setValueKind] = useState<ActionsContextValueKind>("variable");
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [editing, setEditing] = useState(false);

  const location = useMemo(() => activeLocation(target, scopeKind, environment), [environment, scopeKind, target]);
  const listPath = location === null ? "" : actionsContextListPath(location);

  useEffect(() => {
    if (location === null) return;
    const controller = new AbortController();
    void listActionsContextEntries(listPath, controller.signal).then((result) => {
      if (controller.signal.aborted) return;
      if (result.ok) {
        setEntries(result.data);
        setLoadState("ready");
        return;
      }
      setEntries([]);
      setLoadState(isPermissionFailure(result.kind) ? "forbidden" : "unavailable");
    });
    return () => controller.abort();
  }, [listPath, location, reloadKey]);

  function selectScope(nextScope: "repository" | "environment") {
    setScopeKind(nextScope);
    setEnvironment("");
    setEntries([]);
    setError("");
    setNotice("");
    setLoadState(nextScope === "environment" ? "environment-required" : "loading");
    resetEditor();
  }

  function loadEnvironment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const normalized = environmentDraft.trim();
    if (!normalized) return;
    setEntries([]);
    setError("");
    setNotice("");
    setLoadState("loading");
    if (normalized === environment) {
      setReloadKey((current) => current + 1);
    } else {
      setEnvironment(normalized);
    }
    resetEditor();
  }

  function retryLoad() {
    setError("");
    setNotice("");
    setLoadState("loading");
    setReloadKey((current) => current + 1);
  }

  async function saveEntry(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (location === null || !name) return;
    setPending(true);
    setError("");
    setNotice("");
    const normalizedName = name.toUpperCase();
    const result = await putActionsContextEntry(
      actionsContextEntryPath(location, valueKind, normalizedName),
      value,
      session.csrfToken,
    );
    setPending(false);
    if (!result.ok) {
      setError(mutationFailureMessage(copy, result.kind));
      return;
    }
    setEntries((current) => upsertEntry(current, result.data));
    setNotice(copy.saved);
    resetEditor();
  }

  function editEntry(entry: ActionsContextEntry) {
    setEditing(true);
    setName(entry.name);
    setValueKind(entry.secret ? "secret" : "variable");
    setValue(entry.secret ? "" : (entry.value ?? ""));
    setError("");
    setNotice("");
  }

  async function deleteEntry(entry: ActionsContextEntry) {
    if (location === null || !window.confirm(copy.deleteConfirm.replace("{name}", entry.name))) return;
    setPending(true);
    setError("");
    setNotice("");
    const result = await deleteActionsContextEntry(
      actionsContextEntryPath(location, entry.secret ? "secret" : "variable", entry.name),
      session.csrfToken,
    );
    setPending(false);
    if (!result.ok) {
      setError(mutationFailureMessage(copy, result.kind));
      return;
    }
    setEntries((current) => current.filter((item) => !sameEntry(item, entry)));
    setNotice(copy.deleted);
    if (editing && name === entry.name && valueKind === (entry.secret ? "secret" : "variable")) {
      resetEditor();
    }
  }

  function resetEditor() {
    setEditing(false);
    setName("");
    setValue("");
    setValueKind("variable");
  }

  return (
    <div className={styles.stack}>
      {target.kind === "repository" && (
        <div className={styles.scopeControls}>
          <label>
            <span>{copy.scope}</span>
            <select
              disabled={pending}
              onChange={(event) => selectScope(event.target.value as "repository" | "environment")}
              value={scopeKind}
            >
              <option value="repository">{copy.repositoryScope}</option>
              <option value="environment">{copy.environmentScope}</option>
            </select>
          </label>
          {scopeKind === "environment" && (
            <form className={styles.environmentForm} onSubmit={loadEnvironment}>
              <label>
                <span>{copy.environmentName}</span>
                <input
                  list="actions-environment-names"
                  disabled={pending}
                  maxLength={128}
                  onChange={(event) => setEnvironmentDraft(event.target.value)}
                  placeholder={copy.environmentPlaceholder}
                  required
                  value={environmentDraft}
                />
                <datalist id="actions-environment-names">
                  {environmentNames.map((name) => (
                    <option key={name} value={name} />
                  ))}
                </datalist>
              </label>
              <button className={styles.secondaryButton} disabled={pending || !environmentDraft.trim()} type="submit">
                {copy.loadEnvironment}
              </button>
            </form>
          )}
        </div>
      )}

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

      {loadState === "loading" && <p className={styles.muted}>{copy.loading}</p>}
      {loadState === "environment-required" && (
        <div className={styles.empty}>
          <h3>{copy.environmentRequiredTitle}</h3>
          <p>{copy.environmentRequired}</p>
        </div>
      )}
      {loadState === "forbidden" && (
        <div className={styles.callout} role="alert">
          <h3>{copy.forbiddenTitle}</h3>
          <p>{copy.forbiddenBody}</p>
        </div>
      )}
      {loadState === "unavailable" && (
        <div className={styles.callout} role="alert">
          <h3>{copy.unavailableTitle}</h3>
          <p>{copy.unavailableBody}</p>
          <button className={styles.secondaryButton} onClick={retryLoad} type="button">
            {copy.retry}
          </button>
        </div>
      )}
      {loadState === "ready" && (
        <>
          <ActionsContextEntryList
            copy={copy}
            entries={entries}
            locale={locale}
            onDelete={(entry) => void deleteEntry(entry)}
            onEdit={editEntry}
            pending={pending}
          />
          <ActionsContextEntryForm
            copy={copy}
            editing={editing}
            name={name}
            onCancel={resetEditor}
            onNameChange={setName}
            onSubmit={(event) => void saveEntry(event)}
            onValueChange={setValue}
            onValueKindChange={setValueKind}
            pending={pending}
            value={value}
            valueKind={valueKind}
          />
        </>
      )}
    </div>
  );
}

function activeLocation(
  target: ActionsContextTarget,
  scopeKind: "repository" | "environment",
  environment: string,
): ActionsContextLocation | null {
  if (target.kind === "organization") return target;
  if (scopeKind === "repository") {
    return { kind: "repository", owner: target.owner, repository: target.repository };
  }
  if (!environment) return null;
  return { kind: "environment", owner: target.owner, repository: target.repository, environment };
}

function upsertEntry(entries: ActionsContextEntry[], entry: ActionsContextEntry): ActionsContextEntry[] {
  return [...entries.filter((item) => !sameEntry(item, entry)), entry].sort((left, right) => {
    if (left.secret !== right.secret) return left.secret ? 1 : -1;
    return left.name.localeCompare(right.name);
  });
}

function sameEntry(left: ActionsContextEntry, right: ActionsContextEntry): boolean {
  return left.secret === right.secret && left.name.toUpperCase() === right.name.toUpperCase();
}

function isPermissionFailure(kind: ActionsContextFailureKind): boolean {
  return kind === "unauthorized" || kind === "forbidden";
}

function mutationFailureMessage(copy: Dictionary["actionsSettings"], kind: ActionsContextFailureKind): string {
  if (isPermissionFailure(kind)) return copy.mutationForbidden;
  if (kind === "invalid") return copy.invalid;
  return copy.mutationUnavailable;
}
