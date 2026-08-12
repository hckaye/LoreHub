"use client";

import { Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Repository } from "@/lib/api-types";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { scheduleRepositoryDeletion } from "@/lib/repository-lifecycle-client";

import styles from "./repository-lifecycle-settings.module.css";

type Props = {
  dictionary: Dictionary;
  locale: Locale;
  repository: Repository;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function RepositoryDeleteSettings({ dictionary, locale, repository, session }: Props) {
  const router = useRouter();
  const copy = dictionary.repositoryLifecycle;
  const expected = `${repository.owner}/${repository.slug}`;
  const [confirmation, setConfirmation] = useState("");
  const [pending, setPending] = useState(false);
  const [message, setMessage] = useState("");

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPending(true);
    setMessage("");
    const result = await scheduleRepositoryDeletion(repository.owner, repository.slug, confirmation, session.csrfToken);
    if (!result.ok) {
      setPending(false);
      setMessage(mutationFailureMessage(result.kind, dictionary));
      return;
    }
    router.push(`/${locale}/organizations/${encodeURIComponent(repository.owner)}/settings`);
    router.refresh();
  }

  return (
    <form className={styles.form} onSubmit={submit}>
      <div className={styles.summary}>
        <Trash2 aria-hidden="true" size={20} />
        <p>{copy.deleteDescription}</p>
      </div>
      <label>
        <span>{copy.deleteConfirmationLabel.replace("{repository}", expected)}</span>
        <input
          autoComplete="off"
          onChange={(event) => setConfirmation(event.target.value)}
          spellCheck={false}
          value={confirmation}
        />
      </label>
      <div className={styles.actions}>
        <button disabled={pending || confirmation !== expected} type="submit">
          {pending ? copy.deleting : copy.delete}
        </button>
        {message && <span role="alert">{message}</span>}
      </div>
    </form>
  );
}
