export type RevisionStatusState = "pending" | "success" | "failure" | "error";

export type RevisionStatusCreator = {
  id: string;
  username: string;
  displayName: string;
  avatarUrl: string;
};

export type RevisionStatus = {
  id: string;
  revision: string;
  context: string;
  state: RevisionStatusState;
  description: string;
  targetUrl: string;
  creator: RevisionStatusCreator;
  createdAt: string;
};

export type RevisionStatusPage = {
  revision: string;
  state: RevisionStatusState;
  statuses: RevisionStatus[];
  history: RevisionStatus[];
  page: number;
  perPage: number;
  totalCount: number;
  hasNext: boolean;
};

export type MergeStatusCheck = {
  context: string;
  state: RevisionStatusState;
  description: string;
  targetUrl: string;
  creator: string;
  updatedAt: string;
  required: boolean;
};
