"use client";

import { FileLock2, LockKeyhole, Search, UnlockKeyhole } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, Repository } from "@/lib/api-types";
import { deleteJsonWithBody, postJson } from "@/lib/auth-client";
import type { FileLock, FileLockPage } from "@/lib/file-locks";
import { formatDateTime } from "@/lib/format";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { brandedAuthUrl, repositoryPath } from "@/lib/routes";

import { EmptyState } from "../ui/empty-state";
import styles from "./file-lock-manager.module.css";

type FileLockManagerProps = {
  dictionary: Dictionary;
  locale: Locale;
  page: FileLockPage;
  repository: Repository;
  session: AuthSession;
};

export function FileLockManager(props: FileLockManagerProps) {
  const copy = props.dictionary.fileLocks;
  const authenticated = props.session.status === "authenticated" ? props.session : null;
  const [locks, setLocks] = useState(props.page.locks);
  const [path, setPath] = useState("");
  const [filter, setFilter] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [messageIsError, setMessageIsError] = useState(false);
  const router = useRouter();
  const visibleLocks = useMemo(() => filterLocks(locks, filter), [filter, locks]);

  function showMessage(value: string, isError: boolean) {
    setMessage(value);
    setMessageIsError(isError);
  }

  function selectBranch(branch: string) {
    const base = repositoryPath(props.locale, props.repository.owner, props.repository.slug, "locks");
    router.push(`${base}?branch=${encodeURIComponent(branch)}`);
  }

  async function acquire(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!authenticated || !props.page.viewerCanLock || !path.trim()) return;
    setBusy("acquire");
    showMessage("", false);
    const result = await postJson<FileLock>(
      apiPath(props.repository),
      { branch: props.page.selectedBranch, path: path.trim() },
      authenticated.csrfToken,
    );
    setBusy(null);
    if (!result.ok) {
      showMessage(lockFailureMessage(result.code, result.kind, props.dictionary), true);
      return;
    }
    setLocks((current) => [...current.filter((lock) => lock.path !== result.data.path), result.data].sort(sortLocks));
    setPath("");
    showMessage(copy.locked, false);
    router.refresh();
  }

  async function release(lock: FileLock) {
    if (!authenticated || !lock.viewerCanUnlock) return;
    setBusy(lock.path);
    showMessage("", false);
    const result = await deleteJsonWithBody<null>(
      apiPath(props.repository),
      { branch: lock.branch, path: lock.path },
      authenticated.csrfToken,
    );
    setBusy(null);
    if (!result.ok) {
      showMessage(lockFailureMessage(result.code, result.kind, props.dictionary), true);
      return;
    }
    setLocks((current) => current.filter((item) => item.branchId !== lock.branchId || item.path !== lock.path));
    showMessage(copy.unlocked, false);
    router.refresh();
  }

  return (
    <div className={styles.stack}>
      {message && (
        <p className={messageIsError ? styles.error : styles.success} role="status">
          {message}
        </p>
      )}
      <section className={styles.panel}>
        <div className={styles.panelHeading}>
          <div>
            <h2>{copy.createTitle}</h2>
            <p>{copy.createDescription}</p>
          </div>
          <LockKeyhole aria-hidden="true" size={20} />
        </div>
        {authenticated && props.page.viewerCanLock ? (
          <form className={styles.lockForm} onSubmit={(event) => void acquire(event)}>
            <label>
              <span>{copy.branch}</span>
              <select onChange={(event) => selectBranch(event.target.value)} value={props.page.selectedBranch}>
                {props.page.branches.map((branch) => (
                  <option key={branch.id} value={branch.name}>
                    {branch.name}
                  </option>
                ))}
              </select>
            </label>
            <label className={styles.pathField}>
              <span>{copy.path}</span>
              <input
                autoComplete="off"
                maxLength={4096}
                onChange={(event) => setPath(event.target.value)}
                placeholder={copy.pathPlaceholder}
                required
                value={path}
              />
            </label>
            <button disabled={busy !== null} type="submit">
              <LockKeyhole aria-hidden="true" size={15} />
              {busy === "acquire" ? copy.locking : copy.lock}
            </button>
          </form>
        ) : (
          <WriteNotice {...props} />
        )}
      </section>
      <section className={styles.panel}>
        <div className={styles.panelHeading}>
          <div>
            <h2>{copy.listTitle}</h2>
            <p>{copy.listDescription}</p>
          </div>
          <strong>{locks.length}</strong>
        </div>
        {locks.length > 0 && (
          <label className={styles.filter}>
            <span className={styles.visuallyHidden}>{copy.filter}</span>
            <Search aria-hidden="true" size={16} />
            <input
              onChange={(event) => setFilter(event.target.value)}
              placeholder={copy.filterPlaceholder}
              type="search"
              value={filter}
            />
          </label>
        )}
        {props.page.truncated && <p className={styles.warning}>{copy.truncated}</p>}
        {locks.length === 0 ? (
          <EmptyState body={copy.noLocks} icon={<FileLock2 aria-hidden="true" />} title={copy.listTitle} />
        ) : visibleLocks.length === 0 ? (
          <p className={styles.noMatches}>{copy.noMatches}</p>
        ) : (
          <div className={styles.lockList}>
            {visibleLocks.map((lock) => (
              <LockRow
                busy={busy === lock.path}
                copy={copy}
                key={`${lock.branchId}:${lock.path}`}
                locale={props.locale}
                lock={lock}
                release={release}
              />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function LockRow({
  busy,
  copy,
  locale,
  lock,
  release,
}: {
  busy: boolean;
  copy: Dictionary["fileLocks"];
  locale: Locale;
  lock: FileLock;
  release(lock: FileLock): Promise<void>;
}) {
  const owner = lock.owner.displayName || lock.owner.username;
  const date = formatDateTime(lock.lockedAt, locale);
  return (
    <article className={styles.lockRow}>
      <FileLock2 aria-hidden="true" size={18} />
      <div>
        <code>{lock.path}</code>
        <p>
          <span>{copy.owner.replace("{owner}", owner)}</span>
          <span>{copy.lockedAt.replace("{date}", date)}</span>
        </p>
      </div>
      {lock.viewerCanUnlock && (
        <button disabled={busy} onClick={() => void release(lock)} type="button">
          <UnlockKeyhole aria-hidden="true" size={15} />
          {busy ? copy.unlocking : copy.unlock}
        </button>
      )}
    </article>
  );
}

function WriteNotice(props: FileLockManagerProps) {
  const copy = props.dictionary.fileLocks;
  if (props.session.status === "anonymous" || props.session.status === "expired") {
    const returnTo = repositoryPath(props.locale, props.repository.owner, props.repository.slug, "locks");
    return (
      <p className={styles.notice}>
        <LockKeyhole aria-hidden="true" size={16} />
        {copy.signInRequired}{" "}
        <Link href={brandedAuthUrl(props.locale, returnTo)}>{props.dictionary.common.signIn}</Link>
      </p>
    );
  }
  return (
    <p className={styles.notice}>
      <LockKeyhole aria-hidden="true" size={16} /> {copy.writeRequired}
    </p>
  );
}

function apiPath(repository: Repository): string {
  return `/api/v1/repositories/${encodeURIComponent(repository.owner)}/${encodeURIComponent(repository.slug)}/locks`;
}

function filterLocks(locks: FileLock[], query: string): FileLock[] {
  const normalized = query.trim().toLocaleLowerCase();
  if (!normalized) return locks;
  return locks.filter((lock) => {
    const values = [lock.path, lock.owner.username, lock.owner.displayName];
    return values.some((value) => value.toLocaleLowerCase().includes(normalized));
  });
}

function sortLocks(left: FileLock, right: FileLock): number {
  return left.path.localeCompare(right.path);
}

function lockFailureMessage(
  code: string | null,
  kind: "unauthorized" | "forbidden" | "invalid" | "conflict" | "unavailable",
  dictionary: Dictionary,
): string {
  if (code === "lock_conflict") return dictionary.fileLocks.conflict;
  if (code === "lock_not_owned") return dictionary.fileLocks.notOwned;
  if (code === "lock_not_found") return dictionary.fileLocks.notFound;
  return mutationFailureMessage(kind, dictionary);
}
