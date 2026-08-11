import type { LoreDiffFile } from "./api-types";

export type ReviewDiffRow = {
  key: string;
  kind: "header" | "context" | "added" | "deleted";
  content: string;
  oldLine: number | null;
  newLine: number | null;
};

const hunkPattern = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

export function parseReviewDiff(file: LoreDiffFile): ReviewDiffRow[] {
  if (!file.patch) return [];
  const rows: ReviewDiffRow[] = [];
  let oldLine = 0;
  let newLine = 0;
  let inHunk = false;
  let key = 0;
  for (const line of file.patch.split("\n")) {
    const hunk = line.match(hunkPattern);
    if (hunk) {
      oldLine = Number.parseInt(hunk[1], 10);
      newLine = Number.parseInt(hunk[2], 10);
      inHunk = true;
      rows.push({ key: `h-${key++}`, kind: "header", content: line, oldLine: null, newLine: null });
      continue;
    }
    if (!inHunk || line === "\\ No newline at end of file" || line === "") continue;
    if (line.startsWith("+")) {
      rows.push({ key: `n-${newLine}-${key++}`, kind: "added", content: line.slice(1), oldLine: null, newLine });
      newLine += 1;
      continue;
    }
    if (line.startsWith("-")) {
      rows.push({ key: `o-${oldLine}-${key++}`, kind: "deleted", content: line.slice(1), oldLine, newLine: null });
      oldLine += 1;
      continue;
    }
    rows.push({ key: `c-${oldLine}-${newLine}-${key++}`, kind: "context", content: line.slice(1), oldLine, newLine });
    oldLine += 1;
    newLine += 1;
  }
  return rows;
}
