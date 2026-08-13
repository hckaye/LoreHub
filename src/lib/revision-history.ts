import type { Locale } from "@/i18n/config";

import type { APIResult, LoreRevision, RevisionHistoryEntry } from "./api-types";
import { getRevision, getRevisionHistory } from "./lorehub-api";

export const revisionHistoryPageSize = 35;

const maxHistoryLimit = 500;
const detailBatchSize = 6;
const shortRevisionLength = 7;

export type RevisionRow = RevisionHistoryEntry & {
  author?: string;
  createdAt?: string;
  message?: string;
};

export type RevisionRowGroup = {
  key: string;
  date: string | null;
  rows: RevisionRow[];
};

export type RevisionHistoryPage = {
  rows: RevisionRow[];
  page: number;
  hasNext: boolean;
};

export type RevisionHistoryQuery = { branch?: string; revision?: string };

export function parseRevisionHistoryPage(value: string | string[] | undefined): number {
  const raw = Array.isArray(value) ? value[0] : value;
  const page = Number.parseInt(raw ?? "1", 10);
  return Number.isFinite(page) && page > 0 ? Math.min(page, maxPage()) : 1;
}

export function revisionHistoryPageHref(basePath: string, query: RevisionHistoryQuery, page: number): string {
  const params = new URLSearchParams();
  if (query.branch) params.set("branch", query.branch);
  if (query.revision) params.set("revision", query.revision);
  if (page > 1) params.set("page", String(page));
  const search = params.toString();
  return search ? `${basePath}?${search}` : basePath;
}

export function shortRevision(revision: string): string {
  return revision.slice(0, shortRevisionLength);
}

export function revisionSubject(message?: string): string {
  return (message ?? "").split("\n")[0].trim();
}

export function revisionBody(message?: string): string {
  const lines = (message ?? "").split("\n").slice(1);
  return lines.join("\n").trim();
}

export function groupRevisionRows(rows: RevisionRow[], locale: Locale): RevisionRowGroup[] {
  const groups: RevisionRowGroup[] = [];
  for (const row of rows) {
    const date = revisionDate(row.createdAt, locale);
    const current = groups.at(-1);
    if (current && current.key === (date ?? "")) {
      current.rows.push(row);
      continue;
    }
    groups.push({ key: date ?? "", date, rows: [row] });
  }
  return groups;
}

export async function loadRevisionHistoryPage(
  owner: string,
  repository: string,
  query: RevisionHistoryQuery,
  page: number,
): Promise<APIResult<RevisionHistoryPage>> {
  const limit = Math.min(page * revisionHistoryPageSize + 1, maxHistoryLimit);
  const history = await getRevisionHistory(owner, repository, { ...query, limit: String(limit) });
  if (!history.ok) {
    return history;
  }
  const start = (page - 1) * revisionHistoryPageSize;
  const entries = history.data.entries.slice(start, start + revisionHistoryPageSize);
  const rows = await describeRevisions(owner, repository, entries);
  return {
    ok: true,
    data: { rows, page, hasNext: history.data.entries.length > start + revisionHistoryPageSize },
  };
}

async function describeRevisions(
  owner: string,
  repository: string,
  entries: RevisionHistoryEntry[],
): Promise<RevisionRow[]> {
  const rows: RevisionRow[] = [];
  for (let index = 0; index < entries.length; index += detailBatchSize) {
    const batch = entries.slice(index, index + detailBatchSize);
    const details = await Promise.all(batch.map((entry) => getRevision(owner, repository, entry.revision)));
    batch.forEach((entry, position) => rows.push(describeRevision(entry, details[position])));
  }
  return rows;
}

function describeRevision(entry: RevisionHistoryEntry, detail: APIResult<LoreRevision>): RevisionRow {
  if (!detail.ok) {
    return { ...entry };
  }
  return { ...entry, author: detail.data.author, createdAt: detail.data.createdAt, message: detail.data.message };
}

function revisionDate(value: string | undefined, locale: Locale): string | null {
  if (!value) {
    return null;
  }
  const time = Date.parse(value);
  if (!Number.isFinite(time)) {
    return null;
  }
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeZone: "UTC" }).format(time);
}

function maxPage(): number {
  return Math.ceil(maxHistoryLimit / revisionHistoryPageSize);
}
