"use client";

import { KanbanSquare, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Project, ProjectList as ProjectListData, ProjectSummary } from "@/lib/api-types";
import { postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
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
  const labels = props.dictionary.projectsPage;
  const canCreate = props.data.viewerCanWrite && props.session.status === "authenticated";
  const open = props.data.projects.filter((project) => project.state === "open");
  const closed = props.data.projects.filter((project) => project.state === "closed");

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

  return (
    <div className={styles.page}>
      {canCreate && (
        <div className={styles.createArea}>
          <button className={styles.primaryButton} onClick={() => setShowForm((current) => !current)} type="button">
            <Plus aria-hidden="true" size={16} />
            {labels.newProject}
          </button>
          {showForm && (
            <form className={styles.createForm} onSubmit={createProject}>
              <h2>{labels.createTitle}</h2>
              {failure && <FlashNotice body={failure} title={props.dictionary.forms.submitFailed} tone="error" />}
              <label htmlFor="project-title">{labels.titleLabel}</label>
              <input
                autoFocus
                id="project-title"
                maxLength={512}
                onChange={(event) => setTitle(event.target.value)}
                placeholder={labels.titlePlaceholder}
                required
                value={title}
              />
              <label htmlFor="project-description">{labels.descriptionLabel}</label>
              <textarea
                id="project-description"
                maxLength={65_536}
                onChange={(event) => setDescription(event.target.value)}
                placeholder={labels.descriptionPlaceholder}
                value={description}
              />
              <div className={styles.formActions}>
                <button className={styles.primaryButton} disabled={creating} type="submit">
                  {creating ? labels.creating : labels.create}
                </button>
                <button className={styles.secondaryButton} onClick={() => setShowForm(false)} type="button">
                  {props.dictionary.common.cancel}
                </button>
              </div>
            </form>
          )}
        </div>
      )}
      {props.data.projects.length === 0 ? (
        <EmptyState body={labels.emptyBody} icon={<KanbanSquare aria-hidden="true" />} title={labels.emptyTitle} />
      ) : (
        <div className={styles.groups}>
          <ProjectGroup label={labels.open} projects={open} {...props} />
          {closed.length > 0 && <ProjectGroup label={labels.closed} projects={closed} {...props} />}
        </div>
      )}
    </div>
  );
}

function ProjectGroup(props: ProjectListProps & { label: string; projects: ProjectSummary[] }) {
  if (props.projects.length === 0) return null;
  return (
    <section className={styles.group}>
      <h2>{props.label}</h2>
      <div className={styles.list}>
        {props.projects.map((project) => (
          <Link
            className={styles.project}
            href={`${repositoryPath(props.locale, props.owner, props.repository, "projects")}/${project.number}`}
            key={project.id}
          >
            <KanbanSquare aria-hidden="true" size={20} />
            <span className={styles.projectBody}>
              <strong>{project.title}</strong>
              {project.description && <span>{project.description}</span>}
              <small>
                {props.dictionary.projectsPage.projectMeta
                  .replace("{columns}", String(project.columnCount))
                  .replace("{items}", String(project.itemCount))}
                {" · "}
                {props.dictionary.projectsPage.createdBy.replace("{author}", project.createdBy)}
              </small>
            </span>
            <span className={styles.number}>#{project.number}</span>
          </Link>
        ))}
      </div>
    </section>
  );
}

function projectsAPIPath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/projects`;
}
