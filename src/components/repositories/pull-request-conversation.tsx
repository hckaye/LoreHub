"use client";

import { GitPullRequest, MessageSquare } from "lucide-react";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession, MergeRequest, MergeRequestComment, Review, ReviewSummary } from "@/lib/api-types";
import { deleteJson, patchJson, postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./pull-request-conversation.module.css";

type PullRequestConversationProps = {
  comments: MergeRequestComment[];
  commentsAvailable: boolean;
  dictionary: Dictionary;
  locale: Locale;
  mergeRequest: MergeRequest;
  owner: string;
  repository: string;
  reviews: ReviewSummary | null;
  session: AuthSession;
};

export function PullRequestConversation(props: PullRequestConversationProps) {
  const router = useRouter();
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const labels = props.dictionary.pullRequestDetail;
  const csrfToken = props.session.status === "authenticated" ? props.session.csrfToken : "";
  const apiPath = mergeRequestAPIPath(props.owner, props.repository, props.mergeRequest.number);

  async function updateMergeRequest(input: Partial<Pick<MergeRequest, "title" | "body" | "state">>) {
    if (!csrfToken) return false;
    setBusy("request");
    setMessage(null);
    setSuccess(null);
    const result = await patchJson<MergeRequest>(apiPath, input, csrfToken, {
      "If-Match": `"${props.mergeRequest.updatedAt}"`,
    });
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return false;
    }
    router.refresh();
    return true;
  }

  async function createComment(body: string) {
    return mutation("new-comment", () => postJson<MergeRequestComment>(`${apiPath}/comments`, { body }, csrfToken));
  }

  async function updateComment(commentID: string, body: string) {
    return mutation(commentID, () =>
      patchJson<MergeRequestComment>(`${apiPath}/comments/${encodeURIComponent(commentID)}`, { body }, csrfToken),
    );
  }

  async function deleteComment(commentID: string) {
    if (!window.confirm(labels.deleteCommentConfirm)) return;
    await mutation(commentID, () =>
      deleteJson<null>(`${apiPath}/comments/${encodeURIComponent(commentID)}`, csrfToken),
    );
  }

  async function submitReview(decision: Review["decision"], body: string) {
    const completed = await mutation("review", () =>
      postJson<Review>(`${apiPath}/reviews`, { decision, body }, csrfToken),
    );
    if (completed) setSuccess(labels.reviewSubmitted);
    return completed;
  }

  async function mutation(
    action: string,
    request: () => Promise<
      | { ok: true; data: unknown }
      | {
          ok: false;
          kind: "unauthorized" | "forbidden" | "invalid" | "conflict" | "unavailable";
          code: string | null;
        }
    >,
  ) {
    if (!csrfToken) return false;
    setBusy(action);
    setMessage(null);
    setSuccess(null);
    const result = await request();
    setBusy(null);
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return false;
    }
    router.refresh();
    return true;
  }

  return (
    <section aria-labelledby="conversation-title" className={styles.conversation}>
      <h2 id="conversation-title">{labels.conversation}</h2>
      {message && <FlashNotice body={message} title={props.dictionary.forms.submitFailed} tone="error" />}
      {success && <FlashNotice body={success} title={success} tone="success" />}
      <PullRequestDescription
        busy={busy === "request"}
        dictionary={props.dictionary}
        locale={props.locale}
        mergeRequest={props.mergeRequest}
        onUpdate={updateMergeRequest}
      />
      <ReviewList dictionary={props.dictionary} reviews={props.reviews} />
      <div className={styles.timeline}>
        {!props.commentsAvailable ? (
          <p className={styles.muted}>{labels.commentsUnavailable}</p>
        ) : (
          props.comments.map((comment) => (
            <Comment
              busy={busy === comment.id}
              comment={comment}
              dictionary={props.dictionary}
              key={comment.id}
              locale={props.locale}
              onDelete={deleteComment}
              onUpdate={updateComment}
            />
          ))
        )}
      </div>
      {props.session.status === "authenticated" && (
        <CommentForm
          busy={busy === "new-comment"}
          dictionary={props.dictionary}
          onSubmit={createComment}
          username={props.session.user.username}
        />
      )}
      {props.mergeRequest.viewerCanReview && props.session.status === "authenticated" && (
        <ReviewForm busy={busy === "review"} dictionary={props.dictionary} onSubmit={submitReview} />
      )}
    </section>
  );
}

type DescriptionProps = {
  busy: boolean;
  dictionary: Dictionary;
  locale: Locale;
  mergeRequest: MergeRequest;
  onUpdate(input: Partial<Pick<MergeRequest, "title" | "body" | "state">>): Promise<boolean>;
};

function PullRequestDescription(props: DescriptionProps) {
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(props.mergeRequest.title);
  const [body, setBody] = useState(props.mergeRequest.body);
  const labels = props.dictionary.pullRequestDetail;

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onUpdate({ title: title.trim(), body })) setEditing(false);
  }

  const activity = mergeRequestActivity(props.mergeRequest, labels, props.locale);
  return (
    <article className={styles.description}>
      <header>
        <GitPullRequest aria-hidden="true" size={18} />
        <strong>{props.mergeRequest.author}</strong>
        <span>{activity}</span>
        {props.mergeRequest.viewerCanUpdate && (
          <button onClick={() => setEditing((value) => !value)} type="button">
            {labels.editPullRequest}
          </button>
        )}
      </header>
      {editing ? (
        <form className={styles.editor} onSubmit={save}>
          <label htmlFor="pull-request-title">{props.dictionary.forms.titleLabel}</label>
          <input
            id="pull-request-title"
            maxLength={512}
            onChange={(event) => setTitle(event.target.value)}
            required
            value={title}
          />
          <label htmlFor="pull-request-body">{props.dictionary.forms.bodyLabel}</label>
          <textarea
            id="pull-request-body"
            maxLength={1_000_000}
            onChange={(event) => setBody(event.target.value)}
            value={body}
          />
          <div className={styles.actions}>
            <button disabled={props.busy} type="submit">
              {labels.saveChanges}
            </button>
            <button data-tone="secondary" onClick={() => setEditing(false)} type="button">
              {props.dictionary.common.cancel}
            </button>
          </div>
        </form>
      ) : (
        <p className={styles.body}>{props.mergeRequest.body || labels.noDescription}</p>
      )}
      {props.mergeRequest.viewerCanUpdate && props.mergeRequest.state !== "merged" && !editing && (
        <button
          className={styles.stateButton}
          disabled={props.busy}
          onClick={() => props.onUpdate({ state: props.mergeRequest.state === "open" ? "closed" : "open" })}
          type="button"
        >
          {props.mergeRequest.state === "open" ? labels.closePullRequest : labels.reopenPullRequest}
        </button>
      )}
    </article>
  );
}

type CommentProps = {
  busy: boolean;
  comment: MergeRequestComment;
  dictionary: Dictionary;
  locale: Locale;
  onDelete(commentID: string): Promise<void>;
  onUpdate(commentID: string, body: string): Promise<boolean>;
};

function Comment(props: CommentProps) {
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState(props.comment.body);
  const labels = props.dictionary.pullRequestDetail;

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onUpdate(props.comment.id, body)) setEditing(false);
  }

  return (
    <article className={styles.comment}>
      <header>
        <MessageSquare aria-hidden="true" size={16} />
        <strong>{props.comment.author}</strong>
        <time dateTime={props.comment.createdAt}>{formatDate(props.comment.createdAt, props.locale)}</time>
        {props.comment.editedAt && <span>{labels.edited}</span>}
        {props.comment.viewerCanUpdate && (
          <span className={styles.commentActions}>
            <button onClick={() => setEditing(true)} type="button">
              {labels.editComment}
            </button>
            <button onClick={() => props.onDelete(props.comment.id)} type="button">
              {labels.deleteComment}
            </button>
          </span>
        )}
      </header>
      {editing ? (
        <form className={styles.editor} onSubmit={save}>
          <textarea
            aria-label={labels.editComment}
            maxLength={1_000_000}
            onChange={(event) => setBody(event.target.value)}
            required
            value={body}
          />
          <div className={styles.actions}>
            <button disabled={props.busy} type="submit">
              {labels.saveChanges}
            </button>
            <button data-tone="secondary" onClick={() => setEditing(false)} type="button">
              {props.dictionary.common.cancel}
            </button>
          </div>
        </form>
      ) : (
        <p className={styles.body}>{props.comment.body}</p>
      )}
    </article>
  );
}

function CommentForm(props: {
  busy: boolean;
  dictionary: Dictionary;
  onSubmit(body: string): Promise<boolean>;
  username: string;
}) {
  const [body, setBody] = useState("");
  const labels = props.dictionary.pullRequestDetail;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onSubmit(body.trim())) setBody("");
  }
  return (
    <form className={styles.commentForm} onSubmit={submit}>
      <strong>{props.username}</strong>
      <label htmlFor="pull-request-comment">{labels.addComment}</label>
      <textarea
        id="pull-request-comment"
        maxLength={1_000_000}
        onChange={(event) => setBody(event.target.value)}
        placeholder={labels.commentPlaceholder}
        required
        value={body}
      />
      <button disabled={props.busy} type="submit">
        {labels.submitComment}
      </button>
    </form>
  );
}

function ReviewForm(props: {
  busy: boolean;
  dictionary: Dictionary;
  onSubmit(decision: Review["decision"], body: string): Promise<boolean>;
}) {
  const [decision, setDecision] = useState<Review["decision"]>("commented");
  const [body, setBody] = useState("");
  const labels = props.dictionary.pullRequestDetail;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await props.onSubmit(decision, body.trim())) setBody("");
  }
  return (
    <form className={styles.reviewForm} onSubmit={submit}>
      <h3>{labels.reviewTitle}</h3>
      <label htmlFor="review-decision">{labels.reviewDecision}</label>
      <select
        id="review-decision"
        onChange={(event) => setDecision(event.target.value as Review["decision"])}
        value={decision}
      >
        <option value="commented">{labels.commented}</option>
        <option value="approved">{labels.approved}</option>
        <option value="changes_requested">{labels.changesRequestedLabel}</option>
      </select>
      <label htmlFor="review-body">{labels.reviewBody}</label>
      <textarea id="review-body" maxLength={100_000} onChange={(event) => setBody(event.target.value)} value={body} />
      <button disabled={props.busy} type="submit">
        {labels.submitReview}
      </button>
    </form>
  );
}

function ReviewList(props: { dictionary: Dictionary; reviews: ReviewSummary | null }) {
  if (!props.reviews) return null;
  const labels = props.dictionary.pullRequestDetail;
  return (
    <div className={styles.reviews}>
      <h3>{labels.reviewSummary}</h3>
      <p>
        {props.reviews.approvals} {labels.approvals} · {props.reviews.changeRequests} {labels.changesRequested}
      </p>
      {props.reviews.currentReviews.length === 0 ? (
        <p className={styles.muted}>{labels.noReviews}</p>
      ) : (
        props.reviews.currentReviews.map((review) => (
          <article key={review.id}>
            <strong>{review.reviewer}</strong> · {reviewDecision(review.decision, labels)}
            {review.body && <p>{review.body}</p>}
          </article>
        ))
      )}
    </div>
  );
}

function mergeRequestActivity(mergeRequest: MergeRequest, labels: Dictionary["pullRequestDetail"], locale: Locale) {
  if (mergeRequest.state === "merged" && mergeRequest.mergedBy && mergeRequest.mergedAt) {
    return labels.mergedBy
      .replace("{author}", mergeRequest.mergedBy)
      .replace("{date}", formatDate(mergeRequest.mergedAt, locale));
  }
  if (mergeRequest.state === "closed" && mergeRequest.closedAt) {
    return labels.closedBy
      .replace("{author}", mergeRequest.author)
      .replace("{date}", formatDate(mergeRequest.closedAt, locale));
  }
  return labels.openedBy
    .replace("{author}", mergeRequest.author)
    .replace("{date}", formatDate(mergeRequest.createdAt, locale));
}

function reviewDecision(decision: Review["decision"], labels: Dictionary["pullRequestDetail"]): string {
  if (decision === "approved") return labels.approved;
  if (decision === "changes_requested") return labels.changesRequestedLabel;
  return labels.commented;
}

function formatDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(new Date(value));
}

function mergeRequestAPIPath(owner: string, repository: string, number: number): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/merge-requests/${number}`;
}
