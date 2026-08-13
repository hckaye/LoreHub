export type ReactionName = "+1" | "-1" | "laugh" | "confused" | "heart" | "hooray" | "rocket" | "eyes";

export type Reaction = {
  reaction: ReactionName;
  count: number;
  viewerReacted: boolean;
};

export const reactionNames = [
  "+1",
  "-1",
  "laugh",
  "confused",
  "heart",
  "hooray",
  "rocket",
  "eyes",
] as const satisfies readonly ReactionName[];

export const reactionEmoji: Record<ReactionName, string> = {
  "+1": "👍",
  "-1": "👎",
  laugh: "😄",
  confused: "😕",
  heart: "❤️",
  hooray: "🎉",
  rocket: "🚀",
  eyes: "👀",
};

export function repositoryReactionsPath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/reactions`;
}
