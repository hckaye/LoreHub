import type { Locale } from "@/i18n/config";

export const repositorySections = [
  "code",
  "issues",
  "pulls",
  "actions",
  "projects",
  "security",
  "insights",
  "settings",
] as const;

export type RepositorySection = (typeof repositorySections)[number];

export function repositoryPath(
  locale: Locale,
  owner: string,
  repository: string,
  section: RepositorySection = "code",
): string {
  const base = `/${locale}/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}`;
  return section === "code" ? base : `${base}/${section}`;
}

export function localizedPath(locale: Locale, ...segments: string[]): string {
  return `/${locale}/${segments.map((segment) => encodeURIComponent(segment)).join("/")}`;
}

export function safeReturnTo(value: string | null | undefined, fallback = "/"): string {
  if (!value || !value.startsWith("/") || value.startsWith("//") || value.includes("\\")) {
    return fallback;
  }
  try {
    const parsed = new URL(value, "https://lorehub.invalid");
    if (parsed.origin !== "https://lorehub.invalid") {
      return fallback;
    }
    return `${parsed.pathname}${parsed.search}`;
  } catch {
    return fallback;
  }
}

export function loginUrl(returnTo: string | null | undefined, signup = false): string {
  const params = new URLSearchParams({ return_to: safeReturnTo(returnTo) });
  if (signup) {
    params.set("prompt", "create");
  }
  return `/auth/login?${params.toString()}`;
}

export function localePathFrom(pathname: string, locale: Locale): string {
  const segments = pathname.split("/");
  if (segments.length > 1) {
    segments[1] = locale;
  }
  return segments.join("/") || `/${locale}`;
}
