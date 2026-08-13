"use client";

import { Tags } from "lucide-react";
import { useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Label, LabelPage } from "@/lib/api-types";
import { createLabel, deleteLabel, updateLabel, type LabelInput } from "@/lib/label-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import { EmptyState } from "../ui/empty-state";
import styles from "./label-manager.module.css";

type LabelManagerProps = {
  count: number;
  data: LabelPage;
  dictionary: Dictionary;
  owner: string;
  repository: string;
  session: AuthSession;
};

const emptyDraft: LabelInput = { name: "", description: "", color: "#0969da" };

export function LabelManager(props: LabelManagerProps) {
  const copy = props.dictionary.labelsPage;
  const [items, setItems] = useState(() => sortLabels(props.data.items));
  const [draft, setDraft] = useState<LabelInput>(emptyDraft);
  const [editID, setEditID] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState<LabelInput>(emptyDraft);
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState<{ error: boolean; text: string } | null>(null);
  const [filter, setFilter] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  const canWrite = props.data.viewerCanWrite && props.session.status === "authenticated";
  const filteredItems = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return items;
    return items.filter(
      (label) =>
        label.name.toLowerCase().includes(query) ||
        label.description.toLowerCase().includes(query),
    );
  }, [items, filter]);

  async function create(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (props.session.status !== "authenticated") return;
    setBusy("create");
    setMessage(null);
    const result = await createLabel(props.owner, props.repository, draft, props.session.csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage({ error: true, text: mutationFailureMessage(result.kind, props.dictionary) });
      return;
    }
    setItems((current) => sortLabels([...current, result.data]));
    setDraft(emptyDraft);
    setShowCreate(false);
    setMessage({ error: false, text: copy.created });
  }

  function startEdit(label: Label) {
    setEditID(label.id);
    setEditDraft({ name: label.name, description: label.description, color: displayColor(label.color) });
    setMessage(null);
  }

  async function save(event: React.FormEvent<HTMLFormElement>, labelID: string) {
    event.preventDefault();
    if (props.session.status !== "authenticated") return;
    setBusy(labelID);
    setMessage(null);
    const result = await updateLabel(props.owner, props.repository, labelID, editDraft, props.session.csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage({ error: true, text: mutationFailureMessage(result.kind, props.dictionary) });
      return;
    }
    setItems((current) => sortLabels(current.map((item) => (item.id === labelID ? result.data : item))));
    setEditID(null);
    setMessage({ error: false, text: copy.updated });
  }

  async function remove(label: Label) {
    if (props.session.status !== "authenticated") return;
    if (!window.confirm(copy.confirmDelete.replace("{name}", label.name))) return;
    setBusy(label.id);
    setMessage(null);
    const result = await deleteLabel(props.owner, props.repository, label.id, props.session.csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage({ error: true, text: mutationFailureMessage(result.kind, props.dictionary) });
      return;
    }
    setItems((current) => current.filter((item) => item.id !== label.id));
    setEditID(null);
    setMessage({ error: false, text: copy.deleted });
  }

  return (
    <div className={styles.manager}>
      <div className={styles.toolbar}>
        <div className={styles.toolbarLeft}>
          <strong>{copy.countWithTotal.replace("{count}", String(props.count))}</strong>
          <input
            aria-label={copy.filterPlaceholder}
            className={styles.filterInput}
            onChange={(event) => setFilter(event.target.value)}
            placeholder={copy.filterPlaceholder}
            type="search"
            value={filter}
          />
        </div>
        {canWrite && (
          <button
            className={styles.newButton}
            disabled={busy !== null}
            onClick={() => setShowCreate((value) => !value)}
            type="button"
          >
            {copy.newLabel}
          </button>
        )}
      </div>
      {canWrite && showCreate && (
        <form className={styles.createForm} onSubmit={create}>
          <h2>{copy.createTitle}</h2>
          <LabelFields copy={copy} draft={draft} idPrefix="new-label" setDraft={setDraft} />
          <button disabled={busy !== null} type="submit">
            {busy === "create" ? copy.creating : copy.create}
          </button>
        </form>
      )}
      {message && (
        <p className={message.error ? styles.error : styles.success} role={message.error ? "alert" : "status"}>
          {message.text}
        </p>
      )}
      {items.length === 0 ? (
        <EmptyState body={copy.emptyBody} icon={<Tags aria-hidden="true" />} title={copy.emptyTitle} />
      ) : filteredItems.length === 0 ? (
        <EmptyState body={copy.filterPlaceholder} icon={<Tags aria-hidden="true" />} title={copy.emptyTitle} />
      ) : (
        <ul className={styles.list}>
          {filteredItems.map((label) => (
            <li key={label.id}>
              {editID === label.id ? (
                <form className={styles.editForm} onSubmit={(event) => save(event, label.id)}>
                  <LabelFields copy={copy} draft={editDraft} idPrefix={`label-${label.id}`} setDraft={setEditDraft} />
                  <div className={styles.actions}>
                    <button disabled={busy !== null} type="submit">
                      {busy === label.id ? copy.saving : copy.save}
                    </button>
                    <button disabled={busy !== null} onClick={() => setEditID(null)} type="button">
                      {copy.cancel}
                    </button>
                  </div>
                </form>
              ) : (
                <LabelRow
                  busy={busy !== null}
                  canWrite={canWrite}
                  copy={copy}
                  label={label}
                  onDelete={() => remove(label)}
                  onEdit={() => startEdit(label)}
                />
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

type LabelCopy = Dictionary["labelsPage"];

function LabelFields(props: {
  copy: LabelCopy;
  draft: LabelInput;
  idPrefix: string;
  setDraft: React.Dispatch<React.SetStateAction<LabelInput>>;
}) {
  function setValue(key: keyof LabelInput, value: string) {
    props.setDraft((current) => ({ ...current, [key]: value }));
  }
  return (
    <div className={styles.fields}>
      <label htmlFor={`${props.idPrefix}-name`}>
        <span>{props.copy.name}</span>
        <input
          id={`${props.idPrefix}-name`}
          maxLength={128}
          onChange={(event) => setValue("name", event.target.value)}
          placeholder={props.copy.namePlaceholder}
          required
          value={props.draft.name}
        />
      </label>
      <label htmlFor={`${props.idPrefix}-description`}>
        <span>{props.copy.descriptionLabel}</span>
        <input
          id={`${props.idPrefix}-description`}
          maxLength={10_000}
          onChange={(event) => setValue("description", event.target.value)}
          placeholder={props.copy.descriptionPlaceholder}
          value={props.draft.description}
        />
      </label>
      <label className={styles.colorField} htmlFor={`${props.idPrefix}-color`}>
        <span>{props.copy.color}</span>
        <input
          id={`${props.idPrefix}-color`}
          onChange={(event) => setValue("color", event.target.value)}
          type="color"
          value={displayColor(props.draft.color)}
        />
      </label>
    </div>
  );
}

function LabelRow(props: {
  busy: boolean;
  canWrite: boolean;
  copy: LabelCopy;
  label: Label;
  onDelete: () => void;
  onEdit: () => void;
}) {
  const background = displayColor(props.label.color);
  return (
    <div className={styles.row}>
      <span
        className={styles.chip}
        style={{ "--label-color": background, "--label-text": labelTextColor(background) } as React.CSSProperties}
      >
        {props.label.name}
      </span>
      <p>{props.label.description}</p>
      {props.canWrite && (
        <div className={styles.actions}>
          <button disabled={props.busy} onClick={props.onEdit} type="button">
            {props.copy.edit}
          </button>
          <button className={styles.deleteButton} disabled={props.busy} onClick={props.onDelete} type="button">
            {props.copy.delete}
          </button>
        </div>
      )}
    </div>
  );
}

function displayColor(color: string): string {
  const value = color.replace(/^#/, "");
  return /^[0-9a-f]{6}$/i.test(value) ? `#${value}` : "#6e7781";
}

function labelTextColor(color: string): "#000" | "#fff" {
  const red = Number.parseInt(color.slice(1, 3), 16);
  const green = Number.parseInt(color.slice(3, 5), 16);
  const blue = Number.parseInt(color.slice(5, 7), 16);
  const luminance = (red * 299 + green * 587 + blue * 114) / 1000;
  return luminance >= 145 ? "#000" : "#fff";
}

function sortLabels(labels: Label[]): Label[] {
  return [...labels].sort((left, right) => left.name.localeCompare(right.name));
}
