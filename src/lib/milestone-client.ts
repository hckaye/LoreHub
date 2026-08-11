"use client";

import type { Milestone, MilestoneSummary } from "./api-types";
import { deleteJson, deleteJsonWithBody, patchJson, postJson, putJson } from "./auth-client";

type MilestoneInput = {
  title: string;
  description: string;
  dueOn: string | null;
};

type MilestoneUpdate = Partial<MilestoneInput> & {
  state?: "open" | "closed";
  expectedVersion: number;
};

export function createMilestone(owner: string, repository: string, input: MilestoneInput, csrfToken: string) {
  return postJson<Milestone>(milestonesPath(owner, repository), input, csrfToken);
}

export function updateMilestone(
  owner: string,
  repository: string,
  number: number,
  input: MilestoneUpdate,
  csrfToken: string,
) {
  return patchJson<Milestone>(`${milestonesPath(owner, repository)}/${number}`, input, csrfToken);
}

export function deleteMilestone(
  owner: string,
  repository: string,
  number: number,
  expectedVersion: number,
  csrfToken: string,
) {
  return deleteJsonWithBody<null>(`${milestonesPath(owner, repository)}/${number}`, { expectedVersion }, csrfToken);
}

export function assignIssueMilestone(
  owner: string,
  repository: string,
  issueNumber: number,
  milestoneNumber: number,
  csrfToken: string,
) {
  return putJson<MilestoneSummary>(
    `${issuesPath(owner, repository)}/${issueNumber}/milestone/${milestoneNumber}`,
    undefined,
    csrfToken,
  );
}

export function removeIssueMilestone(owner: string, repository: string, issueNumber: number, csrfToken: string) {
  return deleteJson<null>(`${issuesPath(owner, repository)}/${issueNumber}/milestone`, csrfToken);
}

function milestonesPath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/milestones`;
}

function issuesPath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/issues`;
}
