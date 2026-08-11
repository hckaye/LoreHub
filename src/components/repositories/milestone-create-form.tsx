"use client";

import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Milestone } from "@/lib/api-types";
import { createMilestone } from "@/lib/milestone-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./milestone-list.module.css";

type MilestoneCreateFormProps = {
  dictionary: Dictionary;
  owner: string;
  repository: string;
  session: AuthSession;
  onCreated(milestone: Milestone): void;
  onCancel(): void;
};

export function MilestoneCreateForm(props: MilestoneCreateFormProps) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [dueOn, setDueOn] = useState("");
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState("");
  const labels = props.dictionary.milestonesPage;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (props.session.status !== "authenticated") return;
    setSaving(true);
    setFailure("");
    const result = await createMilestone(
      props.owner,
      props.repository,
      { title: title.trim(), description: description.trim(), dueOn: dueOn || null },
      props.session.csrfToken,
    );
    setSaving(false);
    if (!result.ok) {
      setFailure(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    props.onCreated(result.data);
  }

  return (
    <form className={styles.form} onSubmit={submit}>
      <h2>{labels.createTitle}</h2>
      {failure && <FlashNotice body={failure} title={props.dictionary.forms.submitFailed} tone="error" />}
      <label>
        <span>{labels.titleLabel}</span>
        <input autoFocus maxLength={255} onChange={(event) => setTitle(event.target.value)} required value={title} />
      </label>
      <label>
        <span>{labels.descriptionLabel}</span>
        <textarea maxLength={65_536} onChange={(event) => setDescription(event.target.value)} value={description} />
      </label>
      <label>
        <span>{labels.dueOn}</span>
        <input onChange={(event) => setDueOn(event.target.value)} type="date" value={dueOn} />
      </label>
      <div className={styles.formActions}>
        <button className={styles.primaryButton} disabled={saving} type="submit">
          {saving ? labels.creating : labels.create}
        </button>
        <button className={styles.secondaryButton} onClick={props.onCancel} type="button">
          {props.dictionary.common.cancel}
        </button>
      </div>
    </form>
  );
}
