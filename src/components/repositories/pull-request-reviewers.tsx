"use client";

import { Check, CircleAlert, Clock3, MessageCircle, UserRound, UsersRound, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { ReviewCandidate, ReviewRequest, ReviewRequestSummary } from "@/lib/api-types";
import { deleteJson, putJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import { FlashNotice } from "../ui/flash-notice";
import { UserAvatar } from "../ui/user-avatar";
import styles from "./pull-request-reviewers.module.css";

type PullRequestReviewersProps = {
  candidates: ReviewCandidate[];
  csrfToken: string;
  dictionary: Dictionary;
  number: number;
  owner: string;
  repository: string;
  summary: ReviewRequestSummary | null;
};

export function PullRequestReviewers(props: PullRequestReviewersProps) {
  const router = useRouter();
  const [selected, setSelected] = useState("");
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const labels = props.dictionary.pullRequestDetail;
  const base = reviewRequestPath(props.owner, props.repository, props.number);
  const available = useMemo(
    () => availableCandidates(props.candidates, props.summary?.items ?? []),
    [props.candidates, props.summary?.items],
  );

  if (!props.summary) {
    return (
      <section aria-labelledby="requested-reviewers-title" className={styles.panel}>
        <h3 id="requested-reviewers-title">{labels.requestedReviewers}</h3>
        <p className={styles.muted}>{labels.reviewRequestsUnavailable}</p>
      </section>
    );
  }
  const summary = props.summary;

  async function addReviewer() {
    const candidate = decodeCandidate(selected);
    if (!candidate || !props.csrfToken) return;
    setBusy(selected);
    setMessage("");
    const result = await putJson<ReviewRequest>(
      `${base}/${candidate.kind === "user" ? "users" : "teams"}/${encodeURIComponent(candidate.slug)}`,
      {},
      props.csrfToken,
    );
    setBusy("");
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    setSelected("");
    router.refresh();
  }

  async function removeReviewer(request: ReviewRequest) {
    if (!props.csrfToken) return;
    const key = `${request.kind}:${request.slug}`;
    setBusy(key);
    setMessage("");
    const result = await deleteJson<null>(
      `${base}/${request.kind === "user" ? "users" : "teams"}/${encodeURIComponent(request.slug)}`,
      props.csrfToken,
    );
    setBusy("");
    if (!result.ok) {
      setMessage(mutationFailureMessage(result.kind, props.dictionary));
      return;
    }
    router.refresh();
  }

  return (
    <section aria-labelledby="requested-reviewers-title" className={styles.panel}>
      <h3 id="requested-reviewers-title">{labels.requestedReviewers}</h3>
      {message && <FlashNotice body={message} title={props.dictionary.forms.submitFailed} tone="error" />}
      {summary.items.length === 0 ? (
        <p className={styles.muted}>{labels.noRequestedReviewers}</p>
      ) : (
        <ul className={styles.list}>
          {summary.items.map((request) => (
            <li key={request.id}>
              <span className={styles.identity}>
                <UserAvatar avatarUrl={request.avatarUrl} name={request.displayName || request.slug} size={28} />
                {request.kind === "team" ? (
                  <UsersRound aria-hidden="true" size={17} />
                ) : (
                  <UserRound aria-hidden="true" size={17} />
                )}
                <span>
                  <strong>{request.displayName || request.slug}</strong>
                  <small>
                    @{request.slug} · {request.kind === "team" ? labels.reviewerTeam : labels.reviewerUser}
                  </small>
                </span>
              </span>
              <span className={styles.status} data-status={request.status}>
                {reviewRequestStatusIcon(request.status)}
                {reviewRequestStatus(request.status, labels)}
              </span>
              {summary.viewerCanManage && (
                <button
                  aria-label={labels.removeReviewRequest.replace("{reviewer}", request.displayName || request.slug)}
                  disabled={busy === `${request.kind}:${request.slug}`}
                  onClick={() => removeReviewer(request)}
                  type="button"
                >
                  <X aria-hidden="true" size={16} />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {summary.viewerCanManage && (
        <div className={styles.add}>
          <label htmlFor="review-request-candidate">{labels.addReviewer}</label>
          <select id="review-request-candidate" onChange={(event) => setSelected(event.target.value)} value={selected}>
            <option value="">{labels.selectReviewer}</option>
            {available.map((candidate) => (
              <option key={`${candidate.kind}:${candidate.slug}`} value={`${candidate.kind}:${candidate.slug}`}>
                {candidate.displayName || candidate.slug} (@{candidate.slug} ·{" "}
                {candidate.kind === "team" ? labels.reviewerTeam : labels.reviewerUser})
              </option>
            ))}
          </select>
          <button disabled={!selected || busy === selected} onClick={addReviewer} type="button">
            {labels.requestReview}
          </button>
          {available.length === 0 && <small className={styles.muted}>{labels.noReviewCandidates}</small>}
        </div>
      )}
    </section>
  );
}

function availableCandidates(candidates: ReviewCandidate[], requests: ReviewRequest[]): ReviewCandidate[] {
  const requested = new Set(requests.map((request) => `${request.kind}:${request.slug.toLowerCase()}`));
  return candidates.filter((candidate) => !requested.has(`${candidate.kind}:${candidate.slug.toLowerCase()}`));
}

function decodeCandidate(value: string): { kind: "user" | "team"; slug: string } | null {
  const separator = value.indexOf(":");
  if (separator < 1) return null;
  const kind = value.slice(0, separator);
  const slug = value.slice(separator + 1);
  if ((kind !== "user" && kind !== "team") || !slug) return null;
  return { kind, slug };
}

function reviewRequestStatus(status: ReviewRequest["status"], labels: Dictionary["pullRequestDetail"]): string {
  if (status === "approved") return labels.approved;
  if (status === "changes_requested") return labels.changesRequestedLabel;
  if (status === "commented") return labels.commented;
  return labels.reviewPending;
}

function reviewRequestStatusIcon(status: ReviewRequest["status"]) {
  if (status === "approved") return <Check aria-hidden="true" size={15} />;
  if (status === "changes_requested") return <CircleAlert aria-hidden="true" size={15} />;
  if (status === "commented") return <MessageCircle aria-hidden="true" size={15} />;
  return <Clock3 aria-hidden="true" size={15} />;
}

function reviewRequestPath(owner: string, repository: string, number: number): string {
  const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`;
  return `${base}/merge-requests/${number}/review-requests`;
}
