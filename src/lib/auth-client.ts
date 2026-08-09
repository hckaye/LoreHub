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

export function classifyMutationStatus(status: number): MutationFailureKind {
  if (status === 401) {
    return "unauthorized";
  }
  if (status === 403) {
    return "forbidden";
  }
  if (status === 409) {
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
    return { ok: true, data: (await response.json()) as T };
  }
  return { ok: false, kind: classifyMutationStatus(response.status), code: await readProblemCode(response) };
}

async function readProblemCode(response: Response): Promise<string | null> {
  try {
    const payload = (await response.json()) as unknown;
    if (isRecord(payload) && isRecord(payload.error) && typeof payload.error.code === "string") {
      return payload.error.code;
    }
  } catch {
    return null;
  }
  return null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
