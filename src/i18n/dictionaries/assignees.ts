export const assignees = {
  en: {
    title: "Assignees",
    manage: "Edit",
    empty: "No one assigned",
    searchLabel: "Find an assignee",
    searchPlaceholder: "Search people",
    noCandidates: "No assignable users found.",
    unavailable: "Assignable users could not be loaded.",
    limit: "An issue can have at most 10 assignees.",
    assignedTo: "Assigned to {username}",
  },
  ja: {
    title: "担当者",
    manage: "編集",
    empty: "担当者はいません",
    searchLabel: "担当者を検索",
    searchPlaceholder: "ユーザーを検索",
    noCandidates: "担当可能なユーザーが見つかりません。",
    unavailable: "担当可能なユーザーを読み込めませんでした。",
    limit: "1件のIssueには10人まで担当者を設定できます。",
    assignedTo: "{username}さんが担当",
  },
} as const;
