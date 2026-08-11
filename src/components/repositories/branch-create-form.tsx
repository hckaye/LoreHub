"use client";

import { FormEvent, useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Branch } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import styles from "./branch-management.module.css";

type BranchCreateFormProps = {
  owner: string;
  repository: string;
  branches: Branch[];
  defaultBranch: string;
  session: Extract<AuthSession, { status: "authenticated" }>;
  dictionary: Dictionary;
  onCreated(branch: Branch): void;
  onMessage(message: string, error: boolean): void;
};

export function BranchCreateForm({
  owner,
  repository,
  branches,
  defaultBranch,
  session,
  dictionary,
  onCreated,
  onMessage,
}: BranchCreateFormProps) {
  const available = useMemo(() => branches.filter((branch) => !branch.archived), [branches]);
  const initialSource = available.find((branch) => branch.name === defaultBranch)?.name ?? available[0]?.name ?? "";
  const [name, setName] = useState("");
  const [category, setCategory] = useState("");
  const [sourceName, setSourceName] = useState(initialSource);
  const [saving, setSaving] = useState(false);
  const copy = dictionary.branchManagement;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const source = available.find((branch) => branch.name === sourceName);
    if (!source || !name.trim()) return;
    setSaving(true);
    onMessage("", false);
    const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/branches`;
    const result = await postJson<Branch>(
      base,
      {
        name: name.trim(),
        category: category.trim(),
        sourceBranch: source.name,
        sourceRevision: source.latestRevision,
      },
      session.csrfToken,
    );
    setSaving(false);
    if (!result.ok) {
      onMessage(mutationFailureMessage(result.kind, dictionary), true);
      return;
    }
    onCreated(result.data);
    setName("");
    setCategory("");
    onMessage(copy.created, false);
  }

  return (
    <form className={styles.createForm} onSubmit={submit}>
      <div className={styles.field}>
        <label htmlFor="new-branch-name">{copy.name}</label>
        <input
          autoComplete="off"
          id="new-branch-name"
          maxLength={255}
          onChange={(event) => setName(event.target.value)}
          placeholder={copy.namePlaceholder}
          required
          value={name}
        />
      </div>
      <div className={styles.field}>
        <label htmlFor="new-branch-source">{copy.source}</label>
        <select id="new-branch-source" onChange={(event) => setSourceName(event.target.value)} value={sourceName}>
          {available.map((branch) => (
            <option key={branch.id} value={branch.name}>
              {branch.name}
            </option>
          ))}
        </select>
      </div>
      <div className={styles.field}>
        <label htmlFor="new-branch-category">{copy.category}</label>
        <input
          autoComplete="off"
          id="new-branch-category"
          maxLength={64}
          onChange={(event) => setCategory(event.target.value)}
          placeholder={copy.categoryPlaceholder}
          value={category}
        />
      </div>
      <button className={styles.primaryButton} disabled={saving || !name.trim() || !sourceName} type="submit">
        {saving ? copy.creating : copy.create}
      </button>
    </form>
  );
}
