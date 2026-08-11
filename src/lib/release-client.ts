"use client";

import type { Release } from "./api-types";
import { deleteJsonWithBody, patchJson, postJson, type MutationResult } from "./auth-client";

export type CreateReleaseInput = {
  tagName: string;
  title: string;
  notes: string;
  sourceBranch: string;
  revision: string;
  state: "draft" | "published";
};

export function createRelease(
  owner: string,
  repository: string,
  input: CreateReleaseInput,
  csrfToken: string,
): Promise<MutationResult<Release>> {
  return postJson(releasesPath(owner, repository), input, csrfToken);
}

export function updateRelease(
  owner: string,
  repository: string,
  releaseID: string,
  input: { title: string; notes: string; expectedVersion: number },
  csrfToken: string,
): Promise<MutationResult<Release>> {
  return patchJson(releasePath(owner, repository, releaseID), input, csrfToken);
}

export function publishRelease(
  owner: string,
  repository: string,
  releaseID: string,
  expectedVersion: number,
  csrfToken: string,
): Promise<MutationResult<Release>> {
  return postJson(releasePath(owner, repository, releaseID, "publish"), { expectedVersion }, csrfToken);
}

export function deleteRelease(
  owner: string,
  repository: string,
  releaseID: string,
  expectedVersion: number,
  csrfToken: string,
): Promise<MutationResult<null>> {
  return deleteJsonWithBody(releasePath(owner, repository, releaseID), { expectedVersion }, csrfToken);
}

export function addReleaseAsset(
  owner: string,
  repository: string,
  releaseID: string,
  input: { name: string; externalUrl: string; expectedVersion: number },
  csrfToken: string,
): Promise<MutationResult<Release>> {
  return postJson(releasePath(owner, repository, releaseID, "assets"), input, csrfToken);
}

export function deleteReleaseAsset(
  owner: string,
  repository: string,
  releaseID: string,
  assetID: string,
  expectedVersion: number,
  csrfToken: string,
): Promise<MutationResult<Release>> {
  return deleteJsonWithBody(
    releasePath(owner, repository, releaseID, "assets", assetID),
    { expectedVersion },
    csrfToken,
  );
}

export function releasesPath(owner: string, repository: string): string {
  return `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/releases`;
}

function releasePath(owner: string, repository: string, releaseID: string, ...segments: string[]): string {
  const suffix = [releaseID, ...segments].map((segment) => encodeURIComponent(segment)).join("/");
  return `${releasesPath(owner, repository)}/${suffix}`;
}
