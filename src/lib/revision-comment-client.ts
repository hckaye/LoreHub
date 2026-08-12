"use client";

import { deleteJson, patchJson, postJson, type MutationResult } from "./auth-client";
import { parseRevisionComment, revisionCommentsAPIPath, type RevisionComment } from "./revision-comments";

export async function createRevisionComment(
  owner: string,
  repository: string,
  revision: string,
  body: string,
  csrfToken: string,
): Promise<MutationResult<RevisionComment>> {
  const result = await postJson<unknown>(revisionCommentsAPIPath(owner, repository, revision), { body }, csrfToken);
  return parseResult(result);
}

export async function updateRevisionComment(
  owner: string,
  repository: string,
  revision: string,
  commentID: string,
  body: string,
  csrfToken: string,
): Promise<MutationResult<RevisionComment>> {
  const path = `${revisionCommentsAPIPath(owner, repository, revision)}/${encodeURIComponent(commentID)}`;
  return parseResult(await patchJson<unknown>(path, { body }, csrfToken));
}

export function deleteRevisionComment(
  owner: string,
  repository: string,
  revision: string,
  commentID: string,
  csrfToken: string,
): Promise<MutationResult<null>> {
  const path = `${revisionCommentsAPIPath(owner, repository, revision)}/${encodeURIComponent(commentID)}`;
  return deleteJson<null>(path, csrfToken);
}

function parseResult(result: MutationResult<unknown>): MutationResult<RevisionComment> {
  if (!result.ok) return result;
  const comment = parseRevisionComment(result.data);
  return comment ? { ok: true, data: comment } : { ok: false, kind: "unavailable", code: "invalid_response" };
}
