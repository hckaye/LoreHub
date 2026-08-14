"use client";

import { useState } from "react";

import { PopupMenu } from "@/components/ui/popup-menu";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { PendingReview, ReviewVerdict } from "@/lib/api-types";
import { abbreviateCount } from "@/lib/format";
import { discardPendingReview, submitPendingReview } from "@/lib/pending-review-client";

import styles from "./review-diff-view.module.css";

type PendingReviewBarProps = {
  owner: string;
  repository: string;
  number: number;
  locale: Locale;
  csrfToken: string;
  dictionary: Dictionary;
  pendingReview: PendingReview;
  onSubmitted: () => void;
  onDiscarded: () => void;
};

const verdicts: ReviewVerdict[] = ["comment", "approve", "request_changes"];

export function PendingReviewBar(props: PendingReviewBarProps) {
  const [body, setBody] = useState(props.pendingReview.body);
  const [verdict, setVerdict] = useState<ReviewVerdict>("comment");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const labels = props.dictionary.pendingReviews;

  const submit = async (close: () => void) => {
    setBusy(true);
    setMessage("");
    const result = await submitPendingReview(
      props.owner,
      props.repository,
      props.number,
      verdict,
      body,
      props.csrfToken,
    );
    setBusy(false);
    if (!result.ok) {
      setMessage(result.kind === "forbidden" ? labels.ownPullRequest : labels.submitFailed);
      return;
    }
    close();
    props.onSubmitted();
  };

  const discard = async (close: () => void) => {
    if (!window.confirm(labels.abandonConfirm)) return;
    setBusy(true);
    setMessage("");
    const result = await discardPendingReview(props.owner, props.repository, props.number, props.csrfToken);
    setBusy(false);
    if (!result.ok) {
      setMessage(labels.submitFailed);
      return;
    }
    close();
    props.onDiscarded();
  };

  return (
    <div className={styles.pendingBar}>
      <span className={styles.pendingCount}>
        {labels.pendingComments.replace("{count}", abbreviateCount(props.pendingReview.commentCount, props.locale))}
      </span>
      <PopupMenu
        className={styles.pendingFinishWrapper}
        panelClassName={styles.pendingPopover}
        panelRole="dialog"
        trigger={labels.finishReview}
        triggerClassName={styles.pendingFinish}
      >
        {(close) => (
          <>
            <textarea
              aria-label={labels.reviewBodyPlaceholder}
              onChange={(event) => setBody(event.target.value)}
              placeholder={labels.reviewBodyPlaceholder}
              rows={4}
              value={body}
            />
            <fieldset className={styles.pendingVerdicts}>
              <legend>{labels.verdict}</legend>
              {verdicts.map((value) => (
                <label key={value}>
                  <input
                    checked={verdict === value}
                    name="pending-review-verdict"
                    onChange={() => setVerdict(value)}
                    type="radio"
                    value={value}
                  />
                  <span>
                    <strong>{verdictLabel(value, labels)}</strong>
                    <em>{verdictHelp(value, labels)}</em>
                  </span>
                </label>
              ))}
            </fieldset>
            <div className={styles.actions}>
              <button disabled={busy} onClick={() => void discard(close)} type="button">
                {labels.abandon}
              </button>
              <button disabled={busy} onClick={() => void submit(close)} type="button">
                {labels.submitReview}
              </button>
            </div>
            <span aria-live="polite" className={styles.message}>
              {message}
            </span>
          </>
        )}
      </PopupMenu>
    </div>
  );
}

function verdictLabel(verdict: ReviewVerdict, labels: Dictionary["pendingReviews"]): string {
  if (verdict === "approve") return labels.approve;
  if (verdict === "request_changes") return labels.requestChanges;
  return labels.comment;
}

function verdictHelp(verdict: ReviewVerdict, labels: Dictionary["pendingReviews"]): string {
  if (verdict === "approve") return labels.approveHelp;
  if (verdict === "request_changes") return labels.requestChangesHelp;
  return labels.commentHelp;
}
