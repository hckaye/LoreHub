import type { Locale } from "@/i18n/config";

export const repositorySections = [
  "code",
  "locks",
  "issues",
  "discussions",
  "labels",
  "pulls",
  "actions",
  "projects",
  "wiki",
  "releases",
  "tags",
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

export function repositoryBranchesPath(locale: Locale, owner: string, repository: string): string {
  return `/${locale}/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/branches`;
}

export function repositoryMilestonesPath(locale: Locale, owner: string, repository: string): string {
  return `/${locale}/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/milestones`;
}

export function repositoryTagsPath(locale: Locale, owner: string, repository: string): string {
  return repositoryPath(locale, owner, repository, "tags");
}

export function actionsAPIPath(owner: string, repository: string, ...segments: string[]): string {
  const base = `/api/v1/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repository)}/actions`;
  return `${base}/${segments.map((segment) => encodeURIComponent(segment)).join("/")}`;
}

export function localizeHref(href: string, locale: Locale): string {
  if (href === "/" || href.startsWith(`/${locale}/`)) {
    return href === "/" ? `/${locale}` : href;
  }
  return `/${locale}${href.startsWith("/") ? href : `/${href}`}`;
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

export function providerLoginUrl(returnTo: string | null | undefined, provider: string, signup = false): string {
  const params = new URLSearchParams({ return_to: safeReturnTo(returnTo), provider });
  if (signup) {
    params.set("prompt", "create");
  }
  return `/auth/login?${params.toString()}`;
}

export function brandedAuthPath(locale: Locale, register = false): string {
  return `/${locale}/auth/${register ? "register" : "login"}`;
}

export function brandedAuthUrl(locale: Locale, returnTo: string | null | undefined, register = false): string {
  return `${brandedAuthPath(locale, register)}?return_to=${encodeURIComponent(safeReturnTo(returnTo))}`;
}

export function localePathFrom(pathname: string, locale: Locale): string {
  const segments = pathname.split("/");
  if (segments.length > 1) {
    segments[1] = locale;
  }
  return segments.join("/") || `/${locale}`;
}
