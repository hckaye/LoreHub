import "server-only";

import { cookies } from "next/headers";

import type { AuthSession, AuthUser } from "./api-types";

const apiOrigin = process.env.LOREHUB_API_URL ?? "http://127.0.0.1:8080";

export async function getAuthSession(): Promise<AuthSession> {
  try {
    const cookieHeader = (await cookies()).toString();
    const headers = new Headers({ Accept: "application/json" });
    if (cookieHeader) {
      headers.set("Cookie", cookieHeader);
    }
    const response = await fetch(new URL("/api/v1/auth/session", apiOrigin), {
      cache: "no-store",
      headers,
      signal: AbortSignal.timeout(4_000),
    });
    if (response.status === 401) {
      return { status: "expired", user: null };
    }
    if (!response.ok) {
      return { status: "unavailable", user: null, reason: "provider" };
    }
    return normalizeSession((await response.json()) as unknown);
  } catch {
    return { status: "unavailable", user: null, reason: "network" };
  }
}

function normalizeSession(payload: unknown): AuthSession {
  if (!isRecord(payload)) {
    return { status: "unavailable", user: null, reason: "provider" };
  }
  const session = isRecord(payload.session) ? payload.session : payload;
  const authenticated =
    payload.authenticated === true || payload.status === "authenticated" || session.authenticated === true;
  if (!authenticated) {
    return { status: "anonymous", user: null };
  }
  const user = normalizeUser(payload.user ?? session.user);
  const csrfToken =
    readString(payload.csrfToken) ??
    readString(payload.csrf_token) ??
    readString(session.csrfToken) ??
    readString(session.csrf_token);
  if (!user || !csrfToken) {
    return { status: "unavailable", user: null, reason: "provider" };
  }
  return { status: "authenticated", user, csrfToken };
}

function normalizeUser(value: unknown): AuthUser | null {
  if (!isRecord(value)) {
    return null;
  }
  const username =
    readString(value.username) ?? readString(value.preferredUsername) ?? readString(value.preferred_username);
  if (!username) {
    return null;
  }
  return {
    id: readString(value.id) ?? readString(value.subject) ?? username,
    username,
    displayName: readString(value.displayName) ?? readString(value.name) ?? username,
    email: readString(value.email),
    avatarUrl: readString(value.avatarUrl) ?? readString(value.avatar_url),
    locale: readString(value.locale),
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function readString(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value : null;
}
