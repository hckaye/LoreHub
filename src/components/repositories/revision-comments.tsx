"use client";

import { MessageSquare, TriangleAlert } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession } from "@/lib/api-types";
import { mutationFailureMessage } from "@/lib/mutation-messages";
import { createRevisionComment, deleteRevisionComment, updateRevisionComment } from "@/lib/revision-comment-client";
import {
  lastRevisionCommentPage,
  revisionCommentPageHref,
  revisionCommentPageSize,
  type RevisionCommentPage,
} from "@/lib/revision-comments";
import { brandedAuthUrl } from "@/lib/routes";

import { RevisionCommentComposer } from "./revision-comment-composer";
import { RevisionCommentList } from "./revision-comment-list";
import styles from "./revision-comments.module.css";

type RevisionCommentsProps = {
  basePath: string;
  comments: RevisionCommentPage | null;
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  readOnly: boolean;
  repository: string;
  revision: string;
  session: AuthSession;
  unavailableReason?: "forbidden" | "unavailable";
};

export function RevisionComments(props: RevisionCommentsProps) {
  const router = useRouter();
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const csrfToken = props.session.status === "authenticated" ? props.session.csrfToken : "";

  function showPage(totalCount: number, last = false) {
    const currentPage = props.comments?.page ?? 1;
    const perPage = props.comments?.perPage ?? revisionCommentPageSize;
    const finalPage = lastRevisionCommentPage(totalCount, perPage);
    const target = last ? finalPage : Math.min(currentPage, finalPage);
    if (target !== currentPage) {
      router.push(revisionCommentPageHref(props.basePath, props.revision, target));
      return;
    }
    router.refresh();
  }

  async function create(body: string): Promise<boolean> {
    if (!csrfToken) return false;
    setBusy("new");
    setMessage(null);
    const result = await createRevisionComment(props.owner, props.repository, props.revision, body, csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return false;
    }
    showPage((props.comments?.totalCount ?? 0) + 1, true);
    return true;
  }

  async function update(commentID: string, body: string): Promise<boolean> {
    if (!csrfToken) return false;
    setBusy(commentID);
    setMessage(null);
    const result = await updateRevisionComment(
      props.owner,
      props.repository,
      props.revision,
      commentID,
      body,
      csrfToken,
    );
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return false;
    }
    router.refresh();
    return true;
  }

  async function remove(commentID: string): Promise<boolean> {
    if (!csrfToken) return false;
    setBusy(commentID);
    setMessage(null);
    const result = await deleteRevisionComment(props.owner, props.repository, props.revision, commentID, csrfToken);
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return false;
    }
    showPage(Math.max(0, (props.comments?.totalCount ?? 1) - 1));
    return true;
  }

  const copy = props.dictionary.revisionComments;
  return (
    <section aria-labelledby="revision-comments-title" className={styles.section}>
      <header className={styles.heading}>
        <MessageSquare aria-hidden="true" />
        <div>
          <h2 id="revision-comments-title">{copy.title}</h2>
          <p>{copy.summary.replace("{count}", String(props.comments?.totalCount ?? 0))}</p>
        </div>
      </header>
      {message && (
        <p className={styles.error} role="alert">
          {message}
        </p>
      )}
      {props.comments ? (
        <RevisionCommentList
          basePath={props.basePath}
          busy={busy}
          comments={props.comments}
          dictionary={props.dictionary}
          locale={props.locale}
          onDelete={remove}
          onUpdate={update}
          revision={props.revision}
        />
      ) : (
        <div className={styles.notice} data-tone="unavailable">
          <TriangleAlert aria-hidden="true" />
          <p>{props.unavailableReason === "forbidden" ? copy.forbidden : copy.unavailable}</p>
        </div>
      )}
      {!props.readOnly && props.session.status === "authenticated" && props.comments && (
        <RevisionCommentComposer busy={busy === "new"} dictionary={props.dictionary} onSubmit={create} />
      )}
      {!props.readOnly && props.session.status !== "authenticated" && props.session.status !== "unavailable" && (
        <div className={styles.signIn}>
          <div>
            <strong>{copy.signIn}</strong>
            <p>{copy.signInBody}</p>
          </div>
          <Link href={brandedAuthUrl(props.locale, revisionCommentPageHref(props.basePath, props.revision, 1))}>
            {props.dictionary.common.signIn}
          </Link>
        </div>
      )}
    </section>
  );
}
