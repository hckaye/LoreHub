"use client";

import { CalendarDays, Pencil, Trash2 } from "lucide-react";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Milestone } from "@/lib/api-types";
import { deleteMilestone, updateMilestone } from "@/lib/milestone-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./milestone-list.module.css";

type MilestoneCardProps = {
  dictionary: Dictionary;
  locale: Locale;
  milestone: Milestone;
  owner: string;
  repository: string;
  session: AuthSession;
  onChange(milestone: Milestone): void;
  onDelete(number: number): void;
};

export function MilestoneCard(props: MilestoneCardProps) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(props.milestone.title);
  const [description, setDescription] = useState(props.milestone.description);
  const [dueOn, setDueOn] = useState(props.milestone.dueOn ?? "");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const labels = props.dictionary.milestonesPage;
  const canWrite = props.milestone.viewerCanWrite && props.session.status === "authenticated";
  const total = props.milestone.openIssueCount + props.milestone.closedIssueCount;
  const percent = total === 0 ? 0 : Math.round((props.milestone.closedIssueCount / total) * 100);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (props.session.status !== "authenticated") return;
    const saved = await mutate({
      title: title.trim(),
      description: description.trim(),
      dueOn: dueOn || null,
      expectedVersion: props.milestone.version,
    });
    if (saved) setEditing(false);
  }

  async function toggleState() {
    await mutate({
      state: props.milestone.state === "open" ? "closed" : "open",
      expectedVersion: props.milestone.version,
    });
  }

  async function remove() {
    if (props.session.status !== "authenticated" || !window.confirm(labels.confirmDelete)) return;
    setBusy(true);
    setFailure("");
    const result = await deleteMilestone(
      props.owner,
      props.repository,
      props.milestone.number,
      props.milestone.version,
      props.session.csrfToken,
    );
    setBusy(false);
    if (!result.ok) {
      setFailure(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    props.onDelete(props.milestone.number);
  }

  async function mutate(input: Parameters<typeof updateMilestone>[3]): Promise<boolean> {
    if (props.session.status !== "authenticated") return false;
    setBusy(true);
    setFailure("");
    const result = await updateMilestone(
      props.owner,
      props.repository,
      props.milestone.number,
      input,
      props.session.csrfToken,
    );
    setBusy(false);
    if (!result.ok) {
      setFailure(mutationFailureMessage(result.kind, props.dictionary));
      return false;
    }
    props.onChange(result.data);
    return true;
  }

  return (
    <article className={styles.card}>
      <header className={styles.cardHeader}>
        <div>
          <h2>{props.milestone.title}</h2>
          <p className={styles.meta}>
            {labels.createdBy.replace("{author}", props.milestone.createdBy)}
            {props.milestone.dueOn && (
              <>
                {" · "}
                <CalendarDays aria-hidden="true" size={14} />
                {labels.dueDate.replace("{date}", formatDueDate(props.milestone.dueOn, props.locale))}
              </>
            )}
          </p>
        </div>
        {canWrite && (
          <div className={styles.cardActions}>
            <button disabled={busy} onClick={toggleState} type="button">
              {props.milestone.state === "open" ? labels.close : labels.reopen}
            </button>
            <button disabled={busy} onClick={() => setEditing((value) => !value)} type="button">
              <Pencil aria-hidden="true" size={15} />
              {labels.edit}
            </button>
            <button className={styles.dangerButton} disabled={busy} onClick={remove} type="button">
              <Trash2 aria-hidden="true" size={15} />
              {labels.delete}
            </button>
          </div>
        )}
      </header>
      {failure && <FlashNotice body={failure} title={props.dictionary.forms.submitFailed} tone="error" />}
      {editing ? (
        <form className={styles.editForm} onSubmit={save}>
          <input maxLength={255} onChange={(event) => setTitle(event.target.value)} required value={title} />
          <textarea maxLength={65_536} onChange={(event) => setDescription(event.target.value)} value={description} />
          <label>
            <span>{labels.dueOn}</span>
            <input onChange={(event) => setDueOn(event.target.value)} type="date" value={dueOn} />
          </label>
          <div className={styles.formActions}>
            <button className={styles.primaryButton} disabled={busy} type="submit">
              {busy ? labels.saving : labels.save}
            </button>
            <button className={styles.secondaryButton} onClick={() => setEditing(false)} type="button">
              {props.dictionary.common.cancel}
            </button>
          </div>
        </form>
      ) : (
        props.milestone.description && <p className={styles.description}>{props.milestone.description}</p>
      )}
      <div className={styles.progress}>
        <div
          aria-label={labels.progress.replace("{percent}", String(percent))}
          aria-valuemax={100}
          aria-valuemin={0}
          aria-valuenow={percent}
          role="progressbar"
        >
          <span style={{ width: `${percent}%` }} />
        </div>
        <p>
          <strong>{labels.progress.replace("{percent}", String(percent))}</strong>
          <span>{labels.openIssues.replace("{count}", String(props.milestone.openIssueCount))}</span>
          <span>{labels.closedIssues.replace("{count}", String(props.milestone.closedIssueCount))}</span>
        </p>
      </div>
    </article>
  );
}

function formatDueDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(locale === "ja" ? "ja-JP" : "en-US", { dateStyle: "medium" }).format(
    new Date(`${value}T00:00:00Z`),
  );
}
