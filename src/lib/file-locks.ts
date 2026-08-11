import type { Branch } from "./api-types";

export type FileLockOwner = {
  id: string;
  username: string;
  displayName: string;
};

export type FileLock = {
  branchId: string;
  branch: string;
  path: string;
  owner: FileLockOwner;
  lockedAt: string;
  viewerCanUnlock: boolean;
};

export type FileLockPage = {
  locks: FileLock[];
  branches: Branch[];
  selectedBranch: string;
  viewerCanLock: boolean;
  truncated: boolean;
};

export function normalizeFileLockPage(value: unknown): FileLockPage | null {
  if (!isRecord(value) || !Array.isArray(value.locks) || !Array.isArray(value.branches)) return null;
  const locks = value.locks.map(normalizeFileLock);
  const branches = value.branches.map(normalizeBranch);
  if (locks.some((lock) => lock === null) || branches.some((branch) => branch === null)) return null;
  if (
    typeof value.selectedBranch !== "string" ||
    typeof value.viewerCanLock !== "boolean" ||
    typeof value.truncated !== "boolean"
  ) {
    return null;
  }
  return {
    locks: locks as FileLock[],
    branches: branches as Branch[],
    selectedBranch: value.selectedBranch,
    viewerCanLock: value.viewerCanLock,
    truncated: value.truncated,
  };
}

function normalizeFileLock(value: unknown): FileLock | null {
  if (!isRecord(value) || !isRecord(value.owner)) return null;
  if (
    !strings(value, ["branchId", "branch", "path", "lockedAt"]) ||
    !strings(value.owner, ["id", "username", "displayName"]) ||
    typeof value.viewerCanUnlock !== "boolean" ||
    Number.isNaN(Date.parse(value.lockedAt as string))
  ) {
    return null;
  }
  return value as FileLock;
}

function normalizeBranch(value: unknown): Branch | null {
  if (!isRecord(value) || !strings(value, ["id", "name", "category", "latestRevision", "creator", "createdAt"])) {
    return null;
  }
  if (typeof value.current !== "boolean" || typeof value.archived !== "boolean") return null;
  return value as Branch;
}

function strings(value: Record<string, unknown>, keys: string[]): boolean {
  return keys.every((key) => typeof value[key] === "string");
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
