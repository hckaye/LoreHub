import type { APIResult, Branch, LoreDiff } from "./api-types";
import { getLoreDiff, getRevisionHistory } from "./lorehub-api";

const revisionPattern = /^[0-9a-f]{64}$/u;
const ancestorSearchLimit = 500;

export type CompareStatus = "identical" | "mergeable" | "unknown";

export type CompareOption = { label: string; value: string };

export type Comparison = { status: CompareStatus | null; diff: APIResult<LoreDiff> | null };

export async function loadComparison(
  owner: string,
  repository: string,
  refs: { base: string; head: string },
  branches: Branch[],
): Promise<Comparison> {
  const base = resolveCompareRef(refs.base, branches);
  const head = resolveCompareRef(refs.head, branches);
  if (!base || !head) {
    return { status: null, diff: null };
  }
  const status = await compareMergeStatus(owner, repository, base, head);
  if (status === "identical") {
    return { status, diff: null };
  }
  return { status, diff: await getLoreDiff(owner, repository, base, head) };
}

export function resolveCompareRef(value: string, branches: Branch[]): string | null {
  const branch = branches.find((entry) => entry.name === value);
  if (branch) {
    return branch.latestRevision;
  }
  return revisionPattern.test(value.toLowerCase()) ? value.toLowerCase() : null;
}

export function compareOptions(branches: Branch[], refs: string[]): CompareOption[] {
  const options = branches.map((branch) => ({ label: branch.name, value: branch.name }));
  for (const ref of refs) {
    if (!options.some((option) => option.value === ref)) {
      options.push({ label: ref.slice(0, 7), value: ref });
    }
  }
  return options;
}

export async function compareMergeStatus(
  owner: string,
  repository: string,
  base: string,
  head: string,
): Promise<CompareStatus> {
  if (base === head) {
    return "identical";
  }
  const history = await getRevisionHistory(owner, repository, {
    revision: head,
    limit: String(ancestorSearchLimit),
  });
  if (!history.ok) {
    return "unknown";
  }
  return history.data.entries.some((entry) => entry.revision === base) ? "mergeable" : "unknown";
}
