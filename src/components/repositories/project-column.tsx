"use client";

import { MoreHorizontal, Plus } from "lucide-react";
import { FormEvent, useState } from "react";

import { PopupMenu } from "@/components/ui/popup-menu";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { ProjectColumn as ProjectColumnData } from "@/lib/api-types";

import type { CreateCardInput } from "./project-board";
import styles from "./project-board.module.css";
import { ProjectCard } from "./project-card";

type ProjectColumnProps = {
  busy: string | null;
  canWrite: boolean;
  column: ProjectColumnData;
  columns: ProjectColumnData[];
  dictionary: Dictionary;
  locale: Locale;
  onAddCard(input: CreateCardInput): Promise<boolean>;
  onDelete(columnID: string): Promise<void>;
  onDeleteCard(itemID: string): Promise<void>;
  onRename(columnID: string, name: string): Promise<boolean>;
  onUpdateCard(itemID: string, input: Record<string, string>): Promise<boolean>;
  owner: string;
  repository: string;
};

export function ProjectColumn(props: ProjectColumnProps) {
  const [renaming, setRenaming] = useState(false);
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState(props.column.name);
  const labels = props.dictionary.projectsPage.board;

  async function rename(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onRename(props.column.id, name.trim())) {
      setRenaming(false);
    }
  }

  return (
    <section className={styles.column}>
      <header className={styles.columnHeader}>
        <h2>{props.column.name}</h2>
        <span>{props.column.items.length}</span>
        {props.canWrite && (
          <PopupMenu
            className={styles.columnMenu}
            panelClassName={styles.menu}
            trigger={<MoreHorizontal aria-hidden="true" size={18} />}
            triggerClassName={styles.iconButton}
            triggerProps={{ "aria-label": labels.renameColumn }}
          >
            {(close) => (
              <>
                <button
                  onClick={() => {
                    close();
                    setRenaming(true);
                  }}
                  role="menuitem"
                  type="button"
                >
                  {labels.renameColumn}
                </button>
                <button
                  className={styles.dangerAction}
                  onClick={() => {
                    close();
                    props.onDelete(props.column.id);
                  }}
                  role="menuitem"
                  type="button"
                >
                  {labels.deleteColumn}
                </button>
              </>
            )}
          </PopupMenu>
        )}
      </header>
      {renaming && (
        <form className={styles.inlineForm} onSubmit={rename}>
          <label htmlFor={`column-name-${props.column.id}`}>{labels.columnName}</label>
          <input
            autoFocus
            id={`column-name-${props.column.id}`}
            maxLength={255}
            onChange={(event) => setName(event.target.value)}
            required
            value={name}
          />
          <div className={styles.formActions}>
            <button
              className={styles.primaryButton}
              disabled={props.busy === `column-${props.column.id}`}
              type="submit"
            >
              {labels.saveColumn}
            </button>
            <button className={styles.iconButton} onClick={() => setRenaming(false)} type="button">
              {props.dictionary.common.cancel}
            </button>
          </div>
        </form>
      )}
      <div className={styles.cards}>
        {props.column.items.length === 0 && !adding && <p className={styles.noCards}>{labels.noCards}</p>}
        {props.column.items.map((item) => (
          <ProjectCard
            busy={props.busy === `card-${item.id}`}
            canWrite={props.canWrite}
            columns={props.columns}
            dictionary={props.dictionary}
            item={item}
            key={item.id}
            locale={props.locale}
            onDelete={props.onDeleteCard}
            onUpdate={props.onUpdateCard}
            owner={props.owner}
            repository={props.repository}
          />
        ))}
      </div>
      {adding && (
        <CardForm
          busy={props.busy === `new-card-${props.column.id}`}
          columnID={props.column.id}
          dictionary={props.dictionary}
          onAdd={props.onAddCard}
          onCancel={() => setAdding(false)}
        />
      )}
      {props.canWrite && !adding && (
        <button className={styles.addCardButton} onClick={() => setAdding(true)} type="button">
          <Plus aria-hidden="true" size={16} />
          {labels.addCard}
        </button>
      )}
    </section>
  );
}

type CardFormProps = {
  busy: boolean;
  columnID: string;
  dictionary: Dictionary;
  onAdd(input: CreateCardInput): Promise<boolean>;
  onCancel(): void;
};

function CardForm(props: CardFormProps) {
  const [kind, setKind] = useState<CreateCardInput["kind"]>("issue");
  const [number, setNumber] = useState("");
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const labels = props.dictionary.projectsPage.board;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const numeric = Number(number);
    const input: CreateCardInput =
      kind === "issue"
        ? { columnId: props.columnID, kind, issueNumber: numeric }
        : kind === "merge_request"
          ? { columnId: props.columnID, kind, mergeRequestNumber: numeric }
          : { columnId: props.columnID, kind, title: title.trim(), body: body.trim() };
    if (await props.onAdd(input)) props.onCancel();
  }

  return (
    <form className={styles.cardForm} onSubmit={submit}>
      <label htmlFor={`card-kind-${props.columnID}`}>{labels.cardType}</label>
      <select
        id={`card-kind-${props.columnID}`}
        onChange={(event) => setKind(event.target.value as CreateCardInput["kind"])}
        value={kind}
      >
        <option value="issue">{labels.issue}</option>
        <option value="merge_request">{labels.pullRequest}</option>
        <option value="draft">{labels.draft}</option>
      </select>
      {kind === "draft" ? (
        <>
          <label htmlFor={`card-title-${props.columnID}`}>{labels.draftTitle}</label>
          <input
            id={`card-title-${props.columnID}`}
            maxLength={512}
            onChange={(event) => setTitle(event.target.value)}
            required
            value={title}
          />
          <label htmlFor={`card-body-${props.columnID}`}>{labels.draftBody}</label>
          <textarea
            id={`card-body-${props.columnID}`}
            maxLength={65_536}
            onChange={(event) => setBody(event.target.value)}
            value={body}
          />
        </>
      ) : (
        <>
          <label htmlFor={`card-number-${props.columnID}`}>
            {kind === "issue" ? labels.issueNumber : labels.pullRequestNumber}
          </label>
          <input
            id={`card-number-${props.columnID}`}
            min={1}
            onChange={(event) => setNumber(event.target.value)}
            required
            type="number"
            value={number}
          />
        </>
      )}
      <div className={styles.formActions}>
        <button className={styles.primaryButton} disabled={props.busy} type="submit">
          {props.busy ? labels.adding : labels.add}
        </button>
        <button className={styles.iconButton} onClick={props.onCancel} type="button">
          {props.dictionary.common.cancel}
        </button>
      </div>
    </form>
  );
}
