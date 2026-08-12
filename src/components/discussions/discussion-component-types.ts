import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { DiscussionComment } from "@/lib/api-types";

export type DiscussionCopy = Dictionary["discussionsPage"];

export type DiscussionCommentHandlers = {
  onAnswer: (commentID: string, accepted: boolean) => Promise<void>;
  onCreate: (body: string, parentId: string | null) => Promise<boolean>;
  onDelete: (commentID: string) => Promise<boolean>;
  onUpdate: (commentID: string, body: string) => Promise<boolean>;
};

export type DiscussionCommentCardProps = DiscussionCommentHandlers & {
  busy: string | null;
  canComment: boolean;
  comment: DiscussionComment;
  copy: DiscussionCopy;
  locale: Locale;
  root: boolean;
};
