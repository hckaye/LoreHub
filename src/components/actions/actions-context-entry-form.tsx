"use client";

import type { FormEvent } from "react";

import type { Dictionary } from "@/i18n";

import styles from "./actions-context-settings.module.css";

export type ActionsContextValueKind = "variable" | "secret";

type ActionsContextEntryFormProps = {
  copy: Dictionary["actionsSettings"];
  editing: boolean;
  name: string;
  pending: boolean;
  value: string;
  valueKind: ActionsContextValueKind;
  onCancel: () => void;
  onNameChange: (value: string) => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onValueChange: (value: string) => void;
  onValueKindChange: (value: ActionsContextValueKind) => void;
};

export function ActionsContextEntryForm({
  copy,
  editing,
  name,
  pending,
  value,
  valueKind,
  onCancel,
  onNameChange,
  onSubmit,
  onValueChange,
  onValueKindChange,
}: ActionsContextEntryFormProps) {
  return (
    <form className={styles.form} onSubmit={onSubmit}>
      <div className={styles.formHeading}>
        <h3>{editing ? copy.overwriteTitle : copy.createTitle}</h3>
        <p>{valueKind === "secret" ? copy.secretValueHelp : copy.variableValueHelp}</p>
      </div>
      <div className={styles.formGrid}>
        <label>
          <span>{copy.type}</span>
          <select
            disabled={editing || pending}
            onChange={(event) => onValueKindChange(event.target.value as ActionsContextValueKind)}
            value={valueKind}
          >
            <option value="variable">{copy.variable}</option>
            <option value="secret">{copy.secret}</option>
          </select>
        </label>
        <label>
          <span>{copy.name}</span>
          <input
            autoCapitalize="none"
            disabled={editing || pending}
            maxLength={100}
            onChange={(event) => onNameChange(event.target.value.toUpperCase())}
            pattern="[A-Za-z_][A-Za-z0-9_]{0,99}"
            required
            spellCheck={false}
            value={name}
          />
        </label>
      </div>
      <label>
        <span>{copy.value}</span>
        {valueKind === "secret" ? (
          <input
            autoComplete="new-password"
            disabled={pending}
            maxLength={1_048_576}
            onChange={(event) => onValueChange(event.target.value)}
            type="password"
            value={value}
          />
        ) : (
          <textarea
            disabled={pending}
            maxLength={1_048_576}
            onChange={(event) => onValueChange(event.target.value)}
            rows={3}
            value={value}
          />
        )}
      </label>
      <div className={styles.formActions}>
        <button className={styles.primaryButton} disabled={pending || !name} type="submit">
          {pending ? copy.saving : valueKind === "secret" ? copy.saveSecret : copy.saveVariable}
        </button>
        {editing && (
          <button className={styles.secondaryButton} disabled={pending} onClick={onCancel} type="button">
            {copy.cancel}
          </button>
        )}
      </div>
    </form>
  );
}
