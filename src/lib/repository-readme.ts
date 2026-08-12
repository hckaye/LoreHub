import { defaultUrlTransform, type UrlTransform } from "react-markdown";

import type { Locale } from "@/i18n/config";

import type { TreeEntry } from "./api-types";
import { repositoryPath } from "./routes";

const readmePriority = new Map([
  ["readme.md", 0],
  ["readme.markdown", 1],
  ["readme", 2],
]);

export type RepositoryReadmeContext = {
  locale: Locale;
  owner: string;
  repository: string;
  revision: string;
  readmePath: string;
  entries: TreeEntry[];
};

export function findRepositoryReadme(entries: TreeEntry[]): TreeEntry | undefined {
  return entries
    .filter((entry) => entry.kind === "file" && readmePriority.has(entry.name.toLowerCase()))
    .sort((left, right) => {
      const leftPriority = readmePriority.get(left.name.toLowerCase()) ?? Number.MAX_SAFE_INTEGER;
      const rightPriority = readmePriority.get(right.name.toLowerCase()) ?? Number.MAX_SAFE_INTEGER;
      return leftPriority - rightPriority || left.name.localeCompare(right.name);
    })[0];
}

export function createRepositoryReadmeURLTransform(context: RepositoryReadmeContext): UrlTransform {
  const directories = new Set(context.entries.filter((entry) => entry.kind === "directory").map((entry) => entry.path));
  return (url, key, node) => transformRepositoryReadmeURL(url, key, node.tagName, context, directories);
}

export function transformRepositoryReadmeURL(
  url: string,
  key: string,
  tagName: string,
  context: RepositoryReadmeContext,
  knownDirectories = new Set<string>(),
): string {
  const safeURL = defaultUrlTransform(url);
  if (!safeURL || isPageURL(safeURL)) {
    return safeURL;
  }
  const reference = splitReference(safeURL);
  if (!reference.path) {
    return safeURL;
  }
  const decodedPath = decodeReferencePath(reference.path);
  if (decodedPath === null) {
    return "";
  }
  const targetPath = resolveRepositoryPath(context.readmePath, decodedPath);
  if (targetPath === null) {
    return "";
  }
  const base = repositoryPath(context.locale, context.owner, context.repository);
  const params = new URLSearchParams({ revision: context.revision });
  if (targetPath) {
    params.set("path", targetPath);
  }
  if (key === "src" || tagName === "img") {
    return `/api/v1/repositories/${encodeURIComponent(context.owner)}/${encodeURIComponent(
      context.repository,
    )}/raw?${params.toString()}`;
  }
  const directory = reference.path.endsWith("/") || knownDirectories.has(targetPath);
  const location = directory ? `${base}?${params.toString()}` : `${base}/blob?${params.toString()}`;
  return `${location}${reference.fragment}`;
}

function isPageURL(url: string): boolean {
  return url.startsWith("#") || url.startsWith("/") || /^[a-z][a-z\d+.-]*:/i.test(url);
}

function splitReference(value: string): { path: string; fragment: string } {
  const queryIndex = value.indexOf("?");
  const fragmentIndex = value.indexOf("#");
  const indexes = [queryIndex, fragmentIndex].filter((index) => index >= 0);
  const pathEnd = indexes.length > 0 ? Math.min(...indexes) : value.length;
  return {
    path: value.slice(0, pathEnd),
    fragment: fragmentIndex >= 0 ? value.slice(fragmentIndex) : "",
  };
}

function decodeReferencePath(value: string): string | null {
  const decoded: string[] = [];
  for (const segment of value.split("/")) {
    try {
      const part = decodeURIComponent(segment);
      if (part.includes("/") || part.includes("\\") || part.includes("\0")) {
        return null;
      }
      decoded.push(part);
    } catch {
      return null;
    }
  }
  return decoded.join("/");
}

function resolveRepositoryPath(readmePath: string, referencePath: string): string | null {
  const resolved = readmePath.split("/").filter(Boolean);
  resolved.pop();
  for (const part of referencePath.split("/")) {
    if (!part || part === ".") {
      continue;
    }
    if (part === "..") {
      if (resolved.length === 0) {
        return null;
      }
      resolved.pop();
      continue;
    }
    resolved.push(part);
  }
  return resolved.join("/");
}
