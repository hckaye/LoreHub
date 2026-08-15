"use client";

import type { APIResult } from "./api-types";

export type MutationFailureKind = "unauthorized" | "forbidden" | "invalid" | "conflict" | "unavailable";

export type MutationResult<T> = { ok: true; data: T } | { ok: false; kind: MutationFailureKind; code: string | null };

export async function postJson<T>(
  path: string,
  input: unknown,
  csrfToken: string,
  extraHeaders: Record<string, string> = {},
): Promise<MutationResult<T>> {
  try {
    const response = await fetch(path, {
      method: "POST",
      credentials: "include",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
        "X-CSRF-Token": csrfToken,
        ...extraHeaders,
      },
      body: JSON.stringify(input),
    });
    return await readMutationResponse<T>(response);
  } catch {
    return { ok: false, kind: "unavailable", code: "network_error" };
  }
}

export async function patchJson<T>(
  path: string,
  input: unknown,
  csrfToken: string,
  extraHeaders: Record<string, string> = {},
): Promise<MutationResult<T>> {
  return jsonMutation("PATCH", path, input, csrfToken, extraHeaders);
}

export async function putJson<T>(path: string, input: unknown, csrfToken: string): Promise<MutationResult<T>> {
  return jsonMutation("PUT", path, input, csrfToken);
}

export async function deleteJson<T>(path: string, csrfToken: string): Promise<MutationResult<T>> {
  return jsonMutation("DELETE", path, undefined, csrfToken);
}

export async function deleteJsonWithBody<T>(
  path: string,
  input: unknown,
  csrfToken: string,
): Promise<MutationResult<T>> {
  return jsonMutation("DELETE", path, input, csrfToken);
}

export async function postLogout(csrfToken: string): Promise<MutationResult<null>> {
  try {
    const response = await fetch("/auth/logout", {
      method: "POST",
      credentials: "include",
      headers: {
        Accept: "application/json",
        "X-CSRF-Token": csrfToken,
      },
    });
    if (response.ok) {
      return { ok: true, data: null };
    }
    return { ok: false, kind: classifyMutationStatus(response.status), code: await readProblemCode(response) };
  } catch {
    return { ok: false, kind: "unavailable", code: "network_error" };
  }
}

export async function postPasswordLogin(input: {
  identifier: string;
  password: string;
}): Promise<MutationResult<{ authenticated: boolean }>> {
  return credentialMutation("/auth/password/login", input);
}

export async function postPasswordRegister(input: {
  username: string;
  email: string;
  password: string;
  locale: string;
}): Promise<MutationResult<{ authenticated: boolean }>> {
  return credentialMutation("/auth/password/register", input);
}

export async function postPasswordResetRequest(input: { email: string }): Promise<MutationResult<unknown>> {
  return credentialMutation("/auth/password/reset-request", input);
}

export async function postPasswordReset(input: {
  token: string;
  newPassword: string;
}): Promise<MutationResult<unknown>> {
  return credentialMutation("/auth/password/reset", input);
}

async function credentialMutation(path: string, input: unknown): Promise<MutationResult<{ authenticated: boolean }>> {
  try {
    const response = await fetch(path, {
      method: "POST",
      credentials: "include",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    });
    return await readMutationResponse<{ authenticated: boolean }>(response);
  } catch {
    return { ok: false, kind: "unavailable", code: "network_error" };
  }
}

export function classifyMutationStatus(status: number): MutationFailureKind {
  if (status === 401) {
    return "unauthorized";
  }
  if (status === 403) {
    return "forbidden";
  }
  if (status === 409 || status === 412) {
    return "conflict";
  }
  if (status >= 400 && status < 500) {
    return "invalid";
  }
  return "unavailable";
}

export function apiResultToMutation<T>(result: APIResult<T>): MutationResult<T> {
  if (result.ok) {
    return result;
  }
  const kind =
    result.reason === "unauthorized" ? "unauthorized" : result.reason === "forbidden" ? "forbidden" : "invalid";
  return { ok: false, kind, code: result.code ?? result.reason };
}

async function readMutationResponse<T>(response: Response): Promise<MutationResult<T>> {
  if (response.ok) {
    if (response.status === 204) {
      return { ok: true, data: null as T };
    }
    return { ok: true, data: (await response.json()) as T };
  }
  return { ok: false, kind: classifyMutationStatus(response.status), code: await readProblemCode(response) };
}

async function jsonMutation<T>(
  method: "PATCH" | "PUT" | "DELETE",
  path: string,
  input: unknown,
  csrfToken: string,
  extraHeaders: Record<string, string> = {},
): Promise<MutationResult<T>> {
  try {
    const headers: Record<string, string> = {
      Accept: "application/json",
      "X-CSRF-Token": csrfToken,
      ...extraHeaders,
    };
    if (input !== undefined) {
      headers["Content-Type"] = "application/json";
    }
    const response = await fetch(path, {
      method,
      credentials: "include",
      headers,
      body: input === undefined ? undefined : JSON.stringify(input),
    });
    return await readMutationResponse<T>(response);
  } catch {
    return { ok: false, kind: "unavailable", code: "network_error" };
  }
}

async function readProblemCode(response: Response): Promise<string | null> {
  try {
    const payload = (await response.json()) as unknown;
    if (isRecord(payload) && isRecord(payload.error) && typeof payload.error.code === "string") {
      return payload.error.code;
    }
    if (isRecord(payload) && typeof payload.code === "string") {
      return payload.code;
    }
    if (isRecord(payload) && typeof payload.type === "string") {
      const marker = "/problems/";
      const index = payload.type.lastIndexOf(marker);
      return index >= 0 ? payload.type.slice(index + marker.length) : null;
    }
  } catch {
    return null;
  }
  return null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
