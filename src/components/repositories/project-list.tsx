"use client";

import { KanbanSquare, Plus, Search } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Project, ProjectList as ProjectListData, ProjectSummary } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { repositoryPath } from "@/lib/routes";

import { Blankslate } from "../ui/blankslate";
import { FlashNotice } from "../ui/flash-notice";
import styles from "./project-list.module.css";

type ProjectListProps = {
  data: ProjectListData;
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
  session: AuthSession;
};

export function ProjectList(props: ProjectListProps) {
  const router = useRouter();
  const [creating, setCreating] = useState(false);
  const [showForm, setShowForm] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [state, setState] = useState<"open" | "closed">("open");
  const [search, setSearch] = useState("");
  const labels = props.dictionary.projectsPage;
  const canCreate = props.data.viewerCanWrite && props.session.status === "authenticated";
  const openCount = props.data.projects.filter((project) => project.state === "open").length;
  const closedCount = props.data.projects.length - openCount;
  const visible = useMemo(
    () => props.data.projects.filter((project) => project.state === state && matchesSearch(project, search)),
    [props.data.projects, search, state],
  );

  async function createProject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (props.session.status !== "authenticated" || !title.trim()) return;
    setCreating(true);
    setFailure(null);
    const result = await postJson<Project>(
      projectsAPIPath(props.owner, props.repository),
      { title: title.trim(), description: description.trim(), state: "open" },
      props.session.csrfToken,
    );
    if (!result.ok) {
      setCreating(false);
      setFailure(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    router.push(`${repositoryPath(props.locale, props.owner, props.repository, "projects")}/${result.data.number}`);
    router.refresh();
  }

  const createButton = (
    <button className={styles.primaryButton} onClick={() => setShowForm((current) => !current)} type="button">
      <Plus aria-hidden="true" size={16} />
      {labels.newProject}
    </button>
  );

  if (props.data.projects.length === 0) {
    return (
      <div className={styles.page}>
        {showForm && (
          <CreateProjectForm
            creating={creating}
            description={description}
            dictionary={props.dictionary}
            failure={failure}
            onCancel={() => setShowForm(false)}
            onDescriptionChange={setDescription}
            onSubmit={createProject}
            onTitleChange={setTitle}
            title={title}
          />
        )}
        <Blankslate
          action={canCreate ? createButton : undefined}
          body={labels.emptyBody}
          icon={<KanbanSquare aria-hidden="true" />}
          title={labels.emptyTitle}
        />
      </div>
    );
  }

  return (
    <div className={styles.page}>
      <div className={styles.controls}>
        <div className={styles.filters}>
          <label className={styles.search}>
            <Search aria-hidden="true" size={16} />
            <input
              aria-label={labels.searchLabel}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={labels.searchPlaceholder}
              type="search"
              value={search}
            />
          </label>
          <div className={styles.tabs}>
            <button
              aria-pressed={state === "open"}
              className={styles.tab}
              onClick={() => setState("open")}
              type="button"
            >
              {labels.filterOpen.replace("{count}", String(openCount))}
            </button>
            <button
              aria-pressed={state === "closed"}
              className={styles.tab}
              onClick={() => setState("closed")}
              type="button"
            >
              {labels.filterClosed.replace("{count}", String(closedCount))}
            </button>
          </div>
        </div>
        {canCreate && createButton}
      </div>
      {showForm && (
        <CreateProjectForm
          creating={creating}
          description={description}
          dictionary={props.dictionary}
          failure={failure}
          onCancel={() => setShowForm(false)}
          onDescriptionChange={setDescription}
          onSubmit={createProject}
          onTitleChange={setTitle}
          title={title}
        />
      )}
      <div className={styles.list}>
        {visible.length === 0 ? (
          <p className={styles.noMatches}>{labels.noMatches}</p>
        ) : (
          visible.map((project) => (
            <Link
              className={styles.project}
              href={`${repositoryPath(props.locale, props.owner, props.repository, "projects")}/${project.number}`}
              key={project.id}
            >
              <KanbanSquare aria-hidden="true" size={20} />
              <span className={styles.projectBody}>
                <span className={styles.projectName}>{project.title}</span>
                {project.description && <span>{project.description}</span>}
                <small>
                  {labels.projectMeta
                    .replace("{columns}", String(project.columnCount))
                    .replace("{items}", String(project.itemCount))}
                  {" · "}
                  {labels.createdBy.replace("{author}", project.createdBy)}
                </small>
              </span>
              <span className={styles.number}>#{project.number}</span>
            </Link>
          ))
        )}
      </div>
    </div>
  );
}

type CreateProjectFormProps = {
  creating: boolean;
  description: string;
  dictionary: Dictionary;
  failure: string | null;
  title: string;
  onCancel(): void;
  onDescriptionChange(value: string): void;
  onSubmit(event: FormEvent<HTMLFormElement>): void;
  onTitleChange(value: string): void;
};

function CreateProjectForm(props: CreateProjectFormProps) {
  const labels = props.dictionary.projectsPage;
  return (
    <form className={styles.createForm} onSubmit={props.onSubmit}>
      <h2>{labels.createTitle}</h2>
      {props.failure && <FlashNotice body={props.failure} title={props.dictionary.forms.submitFailed} tone="error" />}
      <label htmlFor="project-title">{labels.titleLabel}</label>
      <input
        autoFocus
        id="project-title"
        maxLength={512}
        onChange={(event) => props.onTitleChange(event.target.value)}
        placeholder={labels.titlePlaceholder}
        required
        value={props.title}
      />
      <label htmlFor="project-description">{labels.descriptionLabel}</label>
      <textarea
        id="project-description"
        maxLength={65_536}
        onChange={(event) => props.onDescriptionChange(event.target.value)}
        placeholder={labels.descriptionPlaceholder}
        value={props.description}
      />
      <div className={styles.formActions}>
        <button className={styles.primaryButton} disabled={props.creating} type="submit">
          {props.creating ? labels.creating : labels.create}
        </button>
        <button className={styles.secondaryButton} onClick={props.onCancel} type="button">
          {props.dictionary.common.cancel}
        </button>
      </div>
    </form>
  );
}

function matchesSearch(project: ProjectSummary, search: string): boolean {
  const query = search.trim().toLowerCase();
  if (!query) return true;
  return project.title.toLowerCase().includes(query) || project.description.toLowerCase().includes(query);
}

function projectsAPIPath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/projects`;
}
