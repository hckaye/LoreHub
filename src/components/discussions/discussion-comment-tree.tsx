"use client";

import type { Locale } from "@/i18n/config";
import type { DiscussionComment } from "@/lib/api-types";

import { DiscussionCommentCard } from "./discussion-comment-card";
import type { DiscussionCommentHandlers, DiscussionCopy } from "./discussion-component-types";

type DiscussionCommentTreeProps = DiscussionCommentHandlers & {
  busy: string | null;
  canComment: boolean;
  comment: DiscussionComment;
  comments: DiscussionComment[];
  copy: DiscussionCopy;
  locale: Locale;
};

export function DiscussionCommentTree(props: DiscussionCommentTreeProps) {
  const children = props.comments.filter((comment) => comment.parentId === props.comment.id);
  const root = !props.comment.parentId || !props.comments.some((item) => item.id === props.comment.parentId);
  return (
    <div>
      <DiscussionCommentCard {...props} root={root} />
      {children.map((comment) => (
        <DiscussionCommentTree {...props} comment={comment} key={comment.id} />
      ))}
    </div>
  );
}
