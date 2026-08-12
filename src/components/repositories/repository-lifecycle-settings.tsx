"use client";

import { Archive, ArchiveRestore } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Repository } from "@/lib/api-types";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { archiveRepository, unarchiveRepository } from "@/lib/repository-lifecycle-client";

import styles from "./repository-lifecycle-settings.module.css";

type Props = {
  dictionary: Dictionary;
  repository: Repository;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function RepositoryLifecycleSettings({ dictionary, repository, session }: Props) {
  const router = useRouter();
  const copy = dictionary.repositoryLifecycle;
  const expected = `${repository.owner}/${repository.slug}`;
  const archived = repository.archivedAt !== null;
  const [confirmation, setConfirmation] = useState("");
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState(false);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setMessage("");
    setError(false);
    const result = archived
      ? await unarchiveRepository(repository.owner, repository.slug, confirmation, session.csrfToken)
      : await archiveRepository(repository.owner, repository.slug, confirmation, session.csrfToken);
    setPending(false);
    if (!result.ok) {
      setError(true);
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return;
    }
    setConfirmation("");
    setMessage(archived ? copy.unarchived : copy.archived);
    router.refresh();
  }

  const description = archived ? copy.unarchiveDescription : copy.archiveDescription;
  const button = archived ? copy.unarchive : copy.archive;
  const pendingButton = archived ? copy.unarchiving : copy.archiving;
  const Icon = archived ? ArchiveRestore : Archive;
  return (
    <form className={styles.form} onSubmit={submit}>
      <div className={styles.summary}>
        <Icon aria-hidden="true" size={20} />
        <p>{description}</p>
      </div>
      <label>
        <span>{copy.confirmationLabel.replace("{repository}", expected)}</span>
        <input
          autoComplete="off"
          onChange={(event) => setConfirmation(event.target.value)}
          spellCheck={false}
          value={confirmation}
        />
      </label>
      <div className={styles.actions}>
        <button disabled={pending || confirmation !== expected} type="submit">
          {pending ? pendingButton : button}
        </button>
        {message && <span role={error ? "alert" : "status"}>{message}</span>}
      </div>
    </form>
  );
}
