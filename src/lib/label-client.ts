"use client";

import type { Label } from "./api-types";
import { deleteJson, patchJson, postJson } from "./auth-client";

export type LabelInput = Pick<Label, "name" | "description" | "color">;

function labelsPath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/labels`;
}

function normalizeInput(input: LabelInput): LabelInput {
  return { ...input, color: input.color.replace(/^#/, "") };
}

export function createLabel(owner: string, repository: string, input: LabelInput, csrfToken: string) {
  return postJson<Label>(labelsPath(owner, repository), normalizeInput(input), csrfToken);
}

export function updateLabel(owner: string, repository: string, labelID: string, input: LabelInput, csrfToken: string) {
  return patchJson<Label>(
    `${labelsPath(owner, repository)}/${encodeURIComponent(labelID)}`,
    normalizeInput(input),
    csrfToken,
  );
}

export function deleteLabel(owner: string, repository: string, labelID: string, csrfToken: string) {
  return deleteJson<null>(`${labelsPath(owner, repository)}/${encodeURIComponent(labelID)}`, csrfToken);
}
