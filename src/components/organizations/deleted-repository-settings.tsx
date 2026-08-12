"use client";

import { RotateCcw, Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, DeletedRepository } from "@/lib/api-types";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { restoreDeletedRepository } from "@/lib/repository-lifecycle-client";

import styles from "./deleted-repository-settings.module.css";

type Props = {
  dictionary: Dictionary;
  locale: Locale;
  repositories: DeletedRepository[];
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function DeletedRepositorySettings({ dictionary, locale, repositories, session }: Props) {
  const router = useRouter();
  const copy = dictionary.repositoryLifecycle;
  const [restoring, setRestoring] = useState("");
  const [message, setMessage] = useState("");
  const [messageIsError, setMessageIsError] = useState(false);

  async function restore(repository: DeletedRepository) {
    setRestoring(repository.id);
    setMessage("");
    setMessageIsError(false);
    const result = await restoreDeletedRepository(repository.owner, repository.slug, session.csrfToken);
    if (!result.ok) {
      setRestoring("");
      setMessageIsError(true);
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return;
    }
    setMessage(copy.restored);
    router.refresh();
  }

  if (repositories.length === 0) {
    return (
      <div className={styles.empty}>
        <Trash2 aria-hidden="true" size={20} />
        <p>{copy.noDeletedRepositories}</p>
      </div>
    );
  }
  return (
    <div className={styles.content}>
      <ul className={styles.list}>
        {repositories.map((repository) => (
          <li key={repository.id}>
            <div>
              <strong>{repository.displayName}</strong>
              <code>
                {repository.owner}/{repository.slug}
              </code>
              <span>{copy.requestedBy.replace("{user}", repository.requestedBy)}</span>
              <span>
                {repository.purging
                  ? copy.purging
                  : copy.purgeScheduled.replace("{date}", formatDate(repository.purgeAfter, locale))}
              </span>
            </div>
            <button
              disabled={repository.purging || restoring !== ""}
              onClick={() => void restore(repository)}
              type="button"
            >
              <RotateCcw aria-hidden="true" size={14} />
              {restoring === repository.id ? copy.restoring : copy.restore}
            </button>
          </li>
        ))}
      </ul>
      {message && (
        <p className={styles.message} data-error={messageIsError} role={messageIsError ? "alert" : "status"}>
          {message}
        </p>
      )}
    </div>
  );
}

function formatDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(locale === "ja" ? "ja-JP" : "en-US", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
