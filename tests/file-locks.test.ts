import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { normalizeFileLockPage } from "../src/lib/file-locks";

const branch = {
  id: "branch-id",
  name: "main",
  category: "",
  latestRevision: "a".repeat(64),
  creator: "alice",
  createdAt: "2026-08-12T03:00:00Z",
  current: true,
  archived: false,
};

const lock = {
  branchId: branch.id,
  branch: branch.name,
  path: "Content/Characters/Hero.uasset",
  owner: { id: "user-id", username: "alice", displayName: "Alice" },
  lockedAt: "2026-08-12T03:00:00Z",
  viewerCanUnlock: true,
};

test("file lock pages accept the exact API response shape", () => {
  const page = normalizeFileLockPage({
    locks: [lock],
    branches: [branch],
    selectedBranch: "main",
    viewerCanLock: true,
    truncated: false,
  });
  assert.equal(page?.locks[0]?.path, lock.path);
  assert.equal(page?.branches[0]?.latestRevision, branch.latestRevision);
});

test("file lock pages reject malformed owner, time, and permission fields", () => {
  const base = {
    locks: [lock],
    branches: [branch],
    selectedBranch: "main",
    viewerCanLock: true,
    truncated: false,
  };
  assert.equal(normalizeFileLockPage({ ...base, locks: [{ ...lock, owner: { username: "alice" } }] }), null);
  assert.equal(normalizeFileLockPage({ ...base, locks: [{ ...lock, lockedAt: "not-a-date" }] }), null);
  assert.equal(normalizeFileLockPage({ ...base, viewerCanLock: "yes" }), null);
});

test("file lock UI sends CSRF-protected Lore lock mutations", async () => {
  const source = await readFile("src/components/repositories/file-lock-manager.tsx", "utf8");
  assert.match(source, /postJson<FileLock>/);
  assert.match(source, /deleteJsonWithBody<null>/);
  assert.match(source, /authenticated\.csrfToken/);
  assert.match(source, /viewerCanLock/);
  assert.match(source, /viewerCanUnlock/);
});
