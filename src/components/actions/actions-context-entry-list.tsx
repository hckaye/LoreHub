"use client";

import { KeyRound, Pencil, Trash2, Variable } from "lucide-react";

import type { Dictionary } from "@/i18n";
import type { ActionsContextEntry } from "@/lib/actions-context-client";

import styles from "./actions-context-settings.module.css";

type ActionsContextEntryListProps = {
  copy: Dictionary["actionsSettings"];
  entries: ActionsContextEntry[];
  locale: string;
  pending: boolean;
  onDelete: (entry: ActionsContextEntry) => void;
  onEdit: (entry: ActionsContextEntry) => void;
};

export function ActionsContextEntryList({
  copy,
  entries,
  locale,
  pending,
  onDelete,
  onEdit,
}: ActionsContextEntryListProps) {
  if (entries.length === 0) {
    return (
      <div className={styles.empty}>
        <h3>{copy.emptyTitle}</h3>
        <p>{copy.emptyBody}</p>
      </div>
    );
  }

  return (
    <div className={styles.tableScroll}>
      <table className={styles.table}>
        <thead>
          <tr>
            <th>{copy.name}</th>
            <th>{copy.type}</th>
            <th>{copy.value}</th>
            <th>{copy.updated}</th>
            <th>
              <span className={styles.visuallyHidden}>{copy.actions}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {entries.map((entry) => (
            <tr key={`${entry.secret ? "secret" : "variable"}-${entry.name}`}>
              <td>
                <code className={styles.name}>{entry.name}</code>
              </td>
              <td>
                <span className={styles.kind}>
                  {entry.secret ? <KeyRound aria-hidden="true" size={15} /> : <Variable aria-hidden="true" size={15} />}
                  {entry.secret ? copy.secret : copy.variable}
                </span>
              </td>
              <td>
                {entry.secret ? (
                  <span className={styles.secretMetadata}>
                    {copy.secretStored}
                    {entry.keyId && (
                      <small>
                        {copy.keyId}: {entry.keyId}
                      </small>
                    )}
                  </span>
                ) : (
                  <code className={styles.value}>{entry.value ?? ""}</code>
                )}
              </td>
              <td>
                <time dateTime={entry.updatedAt}>{formatUpdatedAt(entry.updatedAt, locale)}</time>
              </td>
              <td>
                <div className={styles.rowActions}>
                  <button
                    aria-label={`${copy.overwrite} ${entry.name}`}
                    className={styles.iconButton}
                    disabled={pending}
                    onClick={() => onEdit(entry)}
                    title={copy.overwrite}
                    type="button"
                  >
                    <Pencil aria-hidden="true" size={15} />
                  </button>
                  <button
                    aria-label={`${copy.delete} ${entry.name}`}
                    className={styles.dangerIconButton}
                    disabled={pending}
                    onClick={() => onDelete(entry)}
                    title={copy.delete}
                    type="button"
                  >
                    <Trash2 aria-hidden="true" size={15} />
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function formatUpdatedAt(value: string, locale: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date);
}
