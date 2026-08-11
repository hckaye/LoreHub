"use client";

import { CircleDot, Plus } from "lucide-react";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Project } from "@/lib/api-types";
import { deleteJson, patchJson, postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { repositoryPath } from "@/lib/routes";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./project-board.module.css";
import { ProjectColumn } from "./project-column";

type ProjectBoardProps = {
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  project: Project;
  repository: string;
  session: AuthSession;
};

export type CreateCardInput = {
  columnId: string;
  kind: "issue" | "merge_request" | "draft";
  issueNumber?: number;
  mergeRequestNumber?: number;
  title?: string;
  body?: string;
};

export function ProjectBoard(props: ProjectBoardProps) {
  const router = useRouter();
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [addingColumn, setAddingColumn] = useState(false);
  const [title, setTitle] = useState(props.project.title);
  const [description, setDescription] = useState(props.project.description);
  const [columnName, setColumnName] = useState("");
  const labels = props.dictionary.projectsPage;
  const boardLabels = labels.board;
  const csrfToken = props.session.status === "authenticated" ? props.session.csrfToken : "";
  const apiPath = projectAPIPath(props.owner, props.repository, props.project.number);
  const canWrite = props.project.viewerCanWrite && Boolean(csrfToken);

  async function mutate(action: string, request: () => Promise<MutationResponse>): Promise<boolean> {
    if (!csrfToken) return false;
    setBusy(action);
    setMessage(null);
    const result = await request();
    setBusy(null);
    if (!result.ok) {
      if (result.code === "column_not_empty") {
        setMessage(boardLabels.columnNotEmpty);
      } else {
        setMessage(mutationFailureMessage(result.kind, props.dictionary));
      }
      return false;
    }
    router.refresh();
    return true;
  }

  async function saveProject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const changed = await mutate("project", () =>
      patchJson<Project>(
        apiPath,
        { title: title.trim(), description: description.trim(), state: props.project.state },
        csrfToken,
      ),
    );
    if (changed) setEditing(false);
  }

  async function toggleProjectState() {
    await mutate("project-state", () =>
      patchJson<Project>(
        apiPath,
        {
          title: props.project.title,
          description: props.project.description,
          state: props.project.state === "open" ? "closed" : "open",
        },
        csrfToken,
      ),
    );
  }

  async function deleteProject() {
    if (!window.confirm(boardLabels.confirmDeleteProject)) return;
    const changed = await mutate("project-delete", () => deleteJson<null>(apiPath, csrfToken));
    if (changed) {
      router.push(repositoryPath(props.locale, props.owner, props.repository, "projects"));
      router.refresh();
    }
  }

  async function addColumn(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const changed = await mutate("new-column", () =>
      postJson<Project>(`${apiPath}/columns`, { name: columnName.trim() }, csrfToken),
    );
    if (changed) {
      setColumnName("");
      setAddingColumn(false);
    }
  }

  async function renameColumn(columnID: string, name: string): Promise<boolean> {
    return mutate(`column-${columnID}`, () =>
      patchJson<Project>(`${apiPath}/columns/${encodeURIComponent(columnID)}`, { name }, csrfToken),
    );
  }

  async function deleteColumn(columnID: string): Promise<void> {
    if (!window.confirm(boardLabels.confirmDeleteColumn)) return;
    await mutate(`column-${columnID}`, () =>
      deleteJson<null>(`${apiPath}/columns/${encodeURIComponent(columnID)}`, csrfToken),
    );
  }

  async function addCard(input: CreateCardInput): Promise<boolean> {
    return mutate(`new-card-${input.columnId}`, () => postJson<Project>(`${apiPath}/items`, input, csrfToken));
  }

  async function updateCard(itemID: string, input: Record<string, string>): Promise<boolean> {
    return mutate(`card-${itemID}`, () =>
      patchJson<Project>(`${apiPath}/items/${encodeURIComponent(itemID)}`, input, csrfToken),
    );
  }

  async function deleteCard(itemID: string): Promise<void> {
    if (!window.confirm(boardLabels.confirmDeleteCard)) return;
    await mutate(`card-${itemID}`, () => deleteJson<null>(`${apiPath}/items/${encodeURIComponent(itemID)}`, csrfToken));
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <div className={styles.titleArea}>
          <div className={styles.stateLine}>
            <span className={styles.state} data-state={props.project.state}>
              <CircleDot aria-hidden="true" size={16} />
              {props.project.state === "open" ? props.dictionary.common.open : props.dictionary.common.closed}
            </span>
            <span>#{props.project.number}</span>
            <span>{labels.createdBy.replace("{author}", props.project.createdBy)}</span>
          </div>
          <h1>{props.project.title}</h1>
          {props.project.description && <p>{props.project.description}</p>}
        </div>
        {canWrite && (
          <div className={styles.headerActions}>
            <button className={styles.secondaryButton} onClick={() => setEditing((value) => !value)} type="button">
              {boardLabels.editProject}
            </button>
            <button
              className={styles.secondaryButton}
              disabled={busy === "project-state"}
              onClick={toggleProjectState}
              type="button"
            >
              {props.project.state === "open" ? boardLabels.closeProject : boardLabels.reopenProject}
            </button>
            <button
              className={styles.dangerButton}
              disabled={busy === "project-delete"}
              onClick={deleteProject}
              type="button"
            >
              {boardLabels.deleteProject}
            </button>
          </div>
        )}
      </header>
      {message && <FlashNotice body={message} title={props.dictionary.forms.submitFailed} tone="error" />}
      {editing && (
        <form className={styles.projectForm} onSubmit={saveProject}>
          <label htmlFor="project-edit-title">{labels.titleLabel}</label>
          <input
            id="project-edit-title"
            maxLength={512}
            onChange={(event) => setTitle(event.target.value)}
            required
            value={title}
          />
          <label htmlFor="project-edit-description">{labels.descriptionLabel}</label>
          <textarea
            id="project-edit-description"
            maxLength={65_536}
            onChange={(event) => setDescription(event.target.value)}
            value={description}
          />
          <div className={styles.formActions}>
            <button className={styles.primaryButton} disabled={busy === "project"} type="submit">
              {boardLabels.saveProject}
            </button>
            <button className={styles.secondaryButton} onClick={() => setEditing(false)} type="button">
              {props.dictionary.common.cancel}
            </button>
          </div>
        </form>
      )}
      <div className={styles.board}>
        {props.project.columns.map((column) => (
          <ProjectColumn
            busy={busy}
            canWrite={canWrite}
            column={column}
            columns={props.project.columns}
            dictionary={props.dictionary}
            key={column.id}
            locale={props.locale}
            onAddCard={addCard}
            onDelete={deleteColumn}
            onDeleteCard={deleteCard}
            onRename={renameColumn}
            onUpdateCard={updateCard}
            owner={props.owner}
            repository={props.repository}
          />
        ))}
        {canWrite && (
          <aside className={styles.addColumn}>
            {addingColumn ? (
              <form onSubmit={addColumn}>
                <label htmlFor="new-column-name">{boardLabels.columnName}</label>
                <input
                  autoFocus
                  id="new-column-name"
                  maxLength={255}
                  onChange={(event) => setColumnName(event.target.value)}
                  required
                  value={columnName}
                />
                <div className={styles.formActions}>
                  <button className={styles.primaryButton} disabled={busy === "new-column"} type="submit">
                    {boardLabels.addColumn}
                  </button>
                  <button className={styles.iconButton} onClick={() => setAddingColumn(false)} type="button">
                    {props.dictionary.common.cancel}
                  </button>
                </div>
              </form>
            ) : (
              <button className={styles.addColumnButton} onClick={() => setAddingColumn(true)} type="button">
                <Plus aria-hidden="true" size={16} />
                {boardLabels.addColumn}
              </button>
            )}
          </aside>
        )}
      </div>
    </div>
  );
}

type MutationResponse =
  | { ok: true; data: unknown }
  | { ok: false; kind: "unauthorized" | "forbidden" | "invalid" | "conflict" | "unavailable"; code: string | null };

function projectAPIPath(owner: string, repository: string, number: number): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/projects/${number}`;
}
