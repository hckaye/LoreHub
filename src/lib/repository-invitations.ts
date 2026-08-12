import { z } from "zod";

import { deleteJson, postJson, putJson, type MutationResult } from "./auth-client";

export const repositoryInvitationPageSize = 20;
export const repositoryInvitationRoles = ["admin", "maintain", "write", "triage", "read"] as const;

const timestamp = z.string().datetime({ offset: true });
const invitationSchema = z
  .object({
    id: z.string().uuid(),
    organizationId: z.string().uuid(),
    repositoryId: z.string().uuid(),
    owner: z.string().min(1),
    repository: z.string().min(1),
    repositoryDisplayName: z.string(),
    inviteeUserId: z.string().uuid(),
    inviteeUsername: z.string().min(1),
    inviteeDisplayName: z.string(),
    invitedByUserId: z.string().uuid(),
    invitedByUsername: z.string().min(1),
    invitedByDisplayName: z.string(),
    role: z.enum(repositoryInvitationRoles),
    status: z.enum(["pending", "accepted", "declined", "revoked", "expired"]),
    expiresAt: timestamp,
    respondedAt: timestamp.nullable(),
    createdAt: timestamp,
    updatedAt: timestamp,
  })
  .strict();

const invitationPageSchema = z
  .object({
    invitations: z.array(invitationSchema),
    total: z.number().int().nonnegative(),
    page: z.number().int().min(1).max(100_000),
    perPage: z.literal(repositoryInvitationPageSize),
  })
  .strict();

const collaboratorSchema = z
  .object({
    userId: z.string().uuid(),
    username: z.string().min(1),
    displayName: z.string(),
    role: z.enum(repositoryInvitationRoles),
    active: z.boolean(),
    source: z.string().min(1),
  })
  .strict();

const collaboratorPageSchema = z.object({ collaborators: z.array(collaboratorSchema) }).strict();

export type RepositoryInvitation = z.infer<typeof invitationSchema>;
export type RepositoryInvitationPage = z.infer<typeof invitationPageSchema>;
export type RepositoryInvitationRole = (typeof repositoryInvitationRoles)[number];
export type RepositoryCollaborator = z.infer<typeof collaboratorSchema>;
export type RepositoryInvitationFailureKind =
  "unauthorized" | "forbidden" | "not-found" | "invalid" | "conflict" | "unavailable";
export type RepositoryInvitationResult<T> =
  { ok: true; data: T } | { ok: false; kind: RepositoryInvitationFailureKind; code: string | null };

export function repositoryInvitationAdminPath(owner: string, repository: string, page = 1): string {
  const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/invitations`;
  return `${base}?${invitationPageQuery(page)}`;
}

export function repositoryInvitationAccountPath(page = 1): string {
  return `/api/v1/account/repository-invitations?${invitationPageQuery(page)}`;
}

export async function listRepositoryInvitations(
  path: string,
  expectedPage: number,
  signal?: AbortSignal,
): Promise<RepositoryInvitationResult<RepositoryInvitationPage>> {
  try {
    const response = await fetch(path, {
      credentials: "include",
      headers: { Accept: "application/json" },
      signal,
    });
    if (!response.ok) return responseFailure(response);
    const page = parseRepositoryInvitationPage(await response.json(), expectedPage);
    return page ? { ok: true, data: page } : unavailable("invalid_response");
  } catch {
    return unavailable("network_error");
  }
}

export async function createRepositoryInvitation(
  owner: string,
  repository: string,
  username: string,
  role: RepositoryInvitationRole,
  csrfToken: string,
): Promise<RepositoryInvitationResult<RepositoryInvitation>> {
  const base = repositoryInvitationAdminPath(owner, repository).split("?")[0];
  const result = await postJson<unknown>(base, { username, role }, csrfToken);
  return parseMutation(result);
}

export async function listRepositoryCollaborators(
  owner: string,
  repository: string,
  signal?: AbortSignal,
): Promise<RepositoryInvitationResult<RepositoryCollaborator[]>> {
  const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`;
  try {
    const response = await fetch(`${base}/collaborators`, {
      credentials: "include",
      headers: { Accept: "application/json" },
      signal,
    });
    if (!response.ok) return responseFailure(response);
    const page = collaboratorPageSchema.safeParse(await response.json());
    return page.success ? { ok: true, data: page.data.collaborators } : unavailable("invalid_response");
  } catch {
    return unavailable("network_error");
  }
}

export async function respondRepositoryInvitation(
  invitationID: string,
  response: "accept" | "decline",
  csrfToken: string,
): Promise<RepositoryInvitationResult<RepositoryInvitation>> {
  const path = `/api/v1/account/repository-invitations/${encodeURIComponent(invitationID)}/${response}`;
  const result = await postJson<unknown>(path, {}, csrfToken);
  return parseMutation(result);
}

export async function revokeRepositoryInvitation(
  owner: string,
  repository: string,
  invitationID: string,
  csrfToken: string,
): Promise<RepositoryInvitationResult<null>> {
  const base = repositoryInvitationAdminPath(owner, repository).split("?")[0];
  const result = await deleteJson<null>(`${base}/${encodeURIComponent(invitationID)}`, csrfToken);
  return convertMutationFailure(result);
}

export async function updateRepositoryCollaboratorRole(
  owner: string,
  repository: string,
  username: string,
  role: RepositoryInvitationRole,
  csrfToken: string,
): Promise<RepositoryInvitationResult<RepositoryCollaborator>> {
  const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`;
  const path = `${base}/collaborators/${encodeURIComponent(username)}`;
  const result = await putJson<unknown>(path, { role, active: true }, csrfToken);
  if (!result.ok) return mutationFailure(result);
  const collaborator = collaboratorSchema.safeParse(result.data);
  return collaborator.success ? { ok: true, data: collaborator.data } : unavailable("invalid_response");
}

export async function revokeRepositoryCollaborator(
  owner: string,
  repository: string,
  username: string,
  csrfToken: string,
): Promise<RepositoryInvitationResult<RepositoryCollaborator>> {
  const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`;
  const path = `${base}/collaborators/${encodeURIComponent(username)}`;
  const result = await deleteJson<unknown>(path, csrfToken);
  if (!result.ok) return mutationFailure(result);
  const collaborator = collaboratorSchema.safeParse(result.data);
  return collaborator.success ? { ok: true, data: collaborator.data } : unavailable("invalid_response");
}

export function parseRepositoryInvitationPage(value: unknown, expectedPage: number): RepositoryInvitationPage | null {
  const result = invitationPageSchema.safeParse(value);
  if (!result.success || result.data.page !== expectedPage) return null;
  const offset = (result.data.page - 1) * result.data.perPage;
  const expectedLength = Math.max(0, Math.min(result.data.perPage, result.data.total - offset));
  return result.data.invitations.length === expectedLength ? result.data : null;
}

function invitationPageQuery(page: number): string {
  return new URLSearchParams({ page: String(page), per_page: String(repositoryInvitationPageSize) }).toString();
}

function parseMutation(result: MutationResult<unknown>): RepositoryInvitationResult<RepositoryInvitation> {
  if (!result.ok) return mutationFailure(result);
  const invitation = invitationSchema.safeParse(result.data);
  return invitation.success ? { ok: true, data: invitation.data } : unavailable("invalid_response");
}

function convertMutationFailure<T>(result: MutationResult<T>): RepositoryInvitationResult<T> {
  return result.ok ? result : mutationFailure(result);
}

function mutationFailure(result: Exclude<MutationResult<unknown>, { ok: true }>): RepositoryInvitationResult<never> {
  return { ok: false, kind: result.kind, code: result.code };
}

async function responseFailure(response: Response): Promise<RepositoryInvitationResult<never>> {
  return {
    ok: false,
    kind: classifyRepositoryInvitationStatus(response.status),
    code: await readProblemCode(response),
  };
}

export function classifyRepositoryInvitationStatus(status: number): RepositoryInvitationFailureKind {
  if (status === 401) return "unauthorized";
  if (status === 403) return "forbidden";
  if (status === 404) return "not-found";
  if (status === 409) return "conflict";
  if (status >= 400 && status < 500) return "invalid";
  return "unavailable";
}

async function readProblemCode(response: Response): Promise<string | null> {
  try {
    const value: unknown = await response.json();
    if (typeof value !== "object" || value === null || !("error" in value)) return null;
    const error = value.error;
    if (typeof error !== "object" || error === null || !("code" in error)) return null;
    return typeof error.code === "string" ? error.code : null;
  } catch {
    return null;
  }
}

function unavailable<T>(code: string): RepositoryInvitationResult<T> {
  return { ok: false, kind: "unavailable", code };
}
