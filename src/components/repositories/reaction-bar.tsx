"use client";

import { Smile } from "lucide-react";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, Reaction, ReactionName } from "@/lib/api-types";
import { deleteJsonWithBody, putJson } from "@/lib/auth-client";
import { reactionEmoji, reactionNames } from "@/lib/reactions";

import styles from "./reaction-bar.module.css";

export type ReactionSubjectKind = "issue" | "merge_request" | "issue_comment" | "merge_request_comment";

type ReactionMutationResponse = {
  reactions: Reaction[];
};

type LocalReactionState = {
  source: string;
  values: Reaction[];
};

type ReactionBarProps = {
  apiPath: string;
  dictionary: Dictionary;
  reactions?: Reaction[];
  session: AuthSession;
  subjectId: string;
  subjectKind: ReactionSubjectKind;
};

const reactionLabelKeys: Record<ReactionName, keyof Dictionary["reactions"]> = {
  "+1": "plusOne",
  "-1": "minusOne",
  laugh: "laugh",
  confused: "confused",
  heart: "heart",
  hooray: "hooray",
  rocket: "rocket",
  eyes: "eyes",
};

export function ReactionBar(props: ReactionBarProps) {
  const serverReactions = props.reactions ?? [];
  const source = JSON.stringify(serverReactions);
  const [localState, setLocalState] = useState<LocalReactionState | null>(null);
  const [open, setOpen] = useState(false);
  const [pendingReaction, setPendingReaction] = useState<ReactionName | null>(null);
  const [failed, setFailed] = useState(false);
  const copy = props.dictionary.reactions;
  const authenticatedSession = props.session.status === "authenticated" ? props.session : null;
  const authenticated = authenticatedSession !== null;
  const csrfToken = authenticatedSession?.csrfToken ?? "";
  const reactions = localState?.source === source ? localState.values : serverReactions;

  async function toggleReaction(reaction: ReactionName) {
    if (!authenticated || pendingReaction) return;
    const previous = reactions;
    const current = reactions.find((entry) => entry.reaction === reaction);
    const removing = current?.viewerReacted === true;
    const optimistic = applyReaction(reactions, reaction, !removing);
    setLocalState({ source, values: optimistic });
    setPendingReaction(reaction);
    setFailed(false);
    setOpen(false);
    const input = {
      subjectKind: props.subjectKind,
      subjectId: props.subjectId,
      reaction,
    };
    const result = removing
      ? await deleteJsonWithBody<ReactionMutationResponse>(props.apiPath, input, csrfToken)
      : await putJson<ReactionMutationResponse>(props.apiPath, input, csrfToken);
    setPendingReaction(null);
    if (!result.ok) {
      setLocalState({ source, values: previous });
      setFailed(true);
      return;
    }
    setLocalState({ source, values: result.data.reactions ?? optimistic });
  }

  return (
    <div aria-label={copy.title} className={styles.bar} role="group">
      {reactions
        .filter((entry) => entry.count > 0)
        .map((entry) => {
          const label = reactionLabel(copy, entry.reaction);
          const description = entry.viewerReacted
            ? copy.remove.replace("{reaction}", label)
            : copy.react.replace("{reaction}", label);
          return (
            <button
              aria-label={description}
              className={styles.pill}
              data-reacted={entry.viewerReacted}
              disabled={!authenticated || pendingReaction !== null}
              key={entry.reaction}
              onClick={() => toggleReaction(entry.reaction)}
              title={description}
              type="button"
            >
              <span aria-hidden="true">{reactionEmoji[entry.reaction]}</span>
              <span>{entry.count}</span>
            </button>
          );
        })}
      {authenticated && (
        <span className={styles.addWrapper}>
          <button
            aria-expanded={open}
            aria-haspopup="menu"
            aria-label={copy.add}
            className={styles.add}
            disabled={pendingReaction !== null}
            onClick={() => setOpen((value) => !value)}
            title={copy.add}
            type="button"
          >
            <Smile aria-hidden="true" size={16} />
          </button>
          {open && (
            <div className={styles.popover} role="menu">
              {reactionNames.map((reaction) => {
                const label = reactionLabel(copy, reaction);
                return (
                  <button
                    aria-label={copy.react.replace("{reaction}", label)}
                    className={styles.option}
                    key={reaction}
                    onClick={() => toggleReaction(reaction)}
                    role="menuitem"
                    title={label}
                    type="button"
                  >
                    <span aria-hidden="true">{reactionEmoji[reaction]}</span>
                  </button>
                );
              })}
            </div>
          )}
        </span>
      )}
      {failed && (
        <span className={styles.error} role="alert">
          {copy.failed}
        </span>
      )}
    </div>
  );
}

function reactionLabel(copy: Dictionary["reactions"], reaction: ReactionName): string {
  return copy[reactionLabelKeys[reaction]];
}

function applyReaction(reactions: Reaction[], reaction: ReactionName, reacted: boolean): Reaction[] {
  const current = reactions.find((entry) => entry.reaction === reaction);
  if (!reacted) {
    if (!current?.viewerReacted) return reactions;
    if (current.count <= 1) return reactions.filter((entry) => entry.reaction !== reaction);
    return reactions.map((entry) =>
      entry.reaction === reaction ? { ...entry, count: entry.count - 1, viewerReacted: false } : entry,
    );
  }
  if (!current) {
    return [...reactions, { reaction, count: 1, viewerReacted: true }];
  }
  return reactions.map((entry) =>
    entry.reaction === reaction ? { ...entry, count: entry.count + 1, viewerReacted: true } : entry,
  );
}
