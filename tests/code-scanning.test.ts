import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import type { APIResult, CodeScanningAlert, SARIFUploadMetadata } from "../src/lib/api-types";
import { codeScanningAlertRows, codeScanningViewState } from "../src/lib/code-scanning";

const root = new URL("../", import.meta.url);

test("code scanning view distinguishes findings, empty results, access denial, and outages", () => {
  const uploads = success([upload]);
  assert.equal(codeScanningViewState(uploads, success([alert])), "ready");
  assert.equal(codeScanningViewState(uploads, success([])), "empty");
  assert.equal(codeScanningViewState(uploads, failure("forbidden")), "forbidden");
  assert.equal(codeScanningViewState(failure("not-found"), success([alert])), "forbidden");
  assert.equal(codeScanningViewState(uploads, failure("unavailable")), "unavailable");
  assert.equal(codeScanningViewState(failure("invalid"), success([alert])), "unavailable");
});

test("code scanning alerts retain exact SARIF upload metadata", () => {
  const missingUploadAlert = { ...alert, id: "alert-2", uploadId: "missing-upload" };
  const rows = codeScanningAlertRows([alert, missingUploadAlert], [upload]);
  assert.equal(rows[0]?.upload?.revision, "lore-revision-1");
  assert.equal(rows[0]?.upload?.ref, "refs/heads/main");
  assert.equal(rows[0]?.upload?.createdAt, "2026-08-10T12:00:00Z");
  assert.equal(rows[1]?.upload, null);
});

test("security page reads both production code scanning endpoints and renders repository metadata", async () => {
  const [page, client, dashboard] = await Promise.all([
    readText("src/app/[locale]/[owner]/[repository]/security/page.tsx"),
    readText("src/lib/lorehub-api.ts"),
    readText("src/components/repositories/code-scanning-dashboard.tsx"),
  ]);
  assert.match(page, /getSARIFUploads\(owner, repository\)/);
  assert.match(page, /getCodeScanningAlerts\(owner, repository\)/);
  assert.match(client, /\/code-scanning\/sarif-uploads\?limit=100/);
  assert.match(client, /\/code-scanning\/alerts\?per_page=1000/);
  assert.match(dashboard, /upload\.revision/);
  assert.match(dashboard, /upload\.ref/);
  assert.match(dashboard, /upload\?\.createdAt/);
  assert.match(dashboard, /locationLabel\(alert\)/);
});

function success<T>(data: T): APIResult<T> {
  return { ok: true, data };
}

function failure(reason: "not-found" | "forbidden" | "invalid" | "unavailable"): APIResult<never> {
  return { ok: false, reason };
}

async function readText(path: string): Promise<string> {
  return readFile(new URL(path, root), "utf8");
}

const upload: SARIFUploadMetadata = {
  id: "upload-1",
  repositoryId: "repository-1",
  runId: "run-1",
  jobId: "job-1",
  attempt: 1,
  tools: ["CodeQL"],
  revision: "lore-revision-1",
  ref: "refs/heads/main",
  version: "2.1.0",
  documentSize: 1024,
  resultsCount: 1,
  createdAt: "2026-08-10T12:00:00Z",
};

const alert: CodeScanningAlert = {
  id: "alert-1",
  uploadId: upload.id,
  repositoryId: upload.repositoryId,
  tool: "CodeQL",
  ruleId: "js/path-injection",
  level: "error",
  message: "Unsanitized path input",
  path: "src/server.ts",
  startLine: 42,
  createdAt: upload.createdAt,
};
