"use client";

import { FormEvent, useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Branch, Release } from "@/lib/api-types";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { createRelease } from "@/lib/release-client";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./release-list.module.css";

type ReleaseCreateFormProps = {
  branches: Branch[];
  dictionary: Dictionary;
  owner: string;
  repository: string;
  session: AuthSession;
  onCreated(release: Release): void;
  onCancel(): void;
};

export function ReleaseCreateForm(props: ReleaseCreateFormProps) {
  const activeBranches = useMemo(() => props.branches.filter((branch) => !branch.archived), [props.branches]);
  const initialBranch = activeBranches.find((branch) => branch.current) ?? activeBranches[0];
  const [sourceBranch, setSourceBranch] = useState(initialBranch?.name ?? "");
  const [tagName, setTagName] = useState("");
  const [title, setTitle] = useState("");
  const [notes, setNotes] = useState("");
  const [state, setState] = useState<"draft" | "published">("draft");
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState("");
  const labels = props.dictionary.releasesPage;
  const selectedBranch = activeBranches.find((branch) => branch.name === sourceBranch);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (props.session.status !== "authenticated" || !selectedBranch) return;
    setSaving(true);
    setFailure("");
    const result = await createRelease(
      props.owner,
      props.repository,
      {
        tagName: tagName.trim(),
        title: title.trim(),
        notes: notes.trim(),
        sourceBranch: selectedBranch.name,
        revision: selectedBranch.latestRevision,
        state,
      },
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
    <form className={styles.createForm} onSubmit={submit}>
      <h2>{labels.createTitle}</h2>
      {failure && <FlashNotice body={failure} title={props.dictionary.forms.submitFailed} tone="error" />}
      {activeBranches.length === 0 && <p className={styles.formHint}>{labels.noBranches}</p>}
      <div className={styles.formGrid}>
        <label>
          <span>{labels.tagName}</span>
          <input
            autoFocus
            maxLength={128}
            onChange={(event) => setTagName(event.target.value)}
            placeholder="v1.0.0"
            required
            value={tagName}
          />
        </label>
        <label>
          <span>{labels.sourceBranch}</span>
          <select
            disabled={activeBranches.length === 0}
            onChange={(event) => setSourceBranch(event.target.value)}
            required
            value={sourceBranch}
          >
            {activeBranches.map((branch) => (
              <option key={branch.id} value={branch.name}>
                {branch.name}
              </option>
            ))}
          </select>
        </label>
      </div>
      {selectedBranch && (
        <p className={styles.formHint}>
          {labels.pinnedRevision}: <code>{selectedBranch.latestRevision}</code>
        </p>
      )}
      <label>
        <span>{labels.titleLabel}</span>
        <input maxLength={512} onChange={(event) => setTitle(event.target.value)} required value={title} />
      </label>
      <label>
        <span>{labels.notesLabel}</span>
        <textarea maxLength={1_048_576} onChange={(event) => setNotes(event.target.value)} value={notes} />
      </label>
      <label>
        <span>{labels.stateLabel}</span>
        <select onChange={(event) => setState(event.target.value as "draft" | "published")} value={state}>
          <option value="draft">{labels.draft}</option>
          <option value="published">{labels.published}</option>
        </select>
      </label>
      <div className={styles.formActions}>
        <button className={styles.primaryButton} disabled={saving || !selectedBranch} type="submit">
          {saving ? labels.creating : labels.create}
        </button>
        <button className={styles.secondaryButton} onClick={props.onCancel} type="button">
          {props.dictionary.common.cancel}
        </button>
      </div>
    </form>
  );
}
