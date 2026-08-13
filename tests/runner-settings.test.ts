import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { createRunnerRegistrationToken, revokeRunner } from "../src/lib/runner-client";
import {
  normalizeRunnerList,
  normalizeRunnerRegistration,
  runnerConfigureCommand,
  runnerStatus,
  runnersPath,
  type Runner,
} from "../src/lib/runners";

const now = new Date("2026-08-13T12:00:00Z");

const runner: Runner = {
  id: "00000000-0000-4000-8000-000000000010",
  scope: { repositoryId: "00000000-0000-4000-8000-000000000001" },
  name: "linux-builder",
  labels: ["self-hosted", "linux", "x64"],
  credentialExpiresAt: "2027-08-13T12:00:00Z",
  lastUsedAt: null,
  revokedAt: null,
  runnerVersion: "1.2.3",
  lastSeenAt: "2026-08-13T11:59:30Z",
  createdAt: "2026-08-01T09:00:00Z",
};

test("runner API paths follow the repository and organization scopes", () => {
  assert.equal(
    runnersPath({ kind: "repository", owner: "acme/x", repository: "web" }),
    "/api/v1/repositories/acme%2Fx/web/actions/runners",
  );
  assert.equal(
    runnersPath({ kind: "organization", organization: "acme corp" }),
    "/api/v1/organizations/acme%20corp/actions/runners",
  );
});

test("runner status is derived from revocation, credential expiry, and heartbeat", () => {
  assert.equal(runnerStatus(runner, now), "idle");
  assert.equal(runnerStatus({ ...runner, lastSeenAt: "2026-08-13T11:50:00Z" }, now), "offline");
  assert.equal(runnerStatus({ ...runner, lastSeenAt: null }, now), "offline");
  assert.equal(runnerStatus({ ...runner, credentialExpiresAt: "2026-08-01T00:00:00Z" }, now), "expired");
  assert.equal(runnerStatus({ ...runner, revokedAt: "2026-08-12T00:00:00Z" }, now), "revoked");
});

test("runner list normalization keeps server fields and rejects malformed records", () => {
  const runners = normalizeRunnerList({ totalCount: 1, runners: [{ ...runner, scope: { repositoryId: "r1" } }] });
  assert.equal(runners?.length, 1);
  assert.deepEqual(runners?.[0].labels, ["self-hosted", "linux", "x64"]);
  assert.equal(normalizeRunnerList({ runners: [{ ...runner, labels: [7] }] }), null);
  assert.equal(normalizeRunnerList({ runners: {} }), null);
});

test("registration token responses must carry the one-time lhrr_ value", () => {
  const registration = normalizeRunnerRegistration({ token: "lhrr_abc", expiresAt: "2026-08-13T13:00:00Z" });
  assert.deepEqual(registration, { token: "lhrr_abc", expiresAt: "2026-08-13T13:00:00Z" });
  assert.equal(normalizeRunnerRegistration({ token: "lhr_abc", expiresAt: "2026-08-13T13:00:00Z" }), null);
  assert.equal(normalizeRunnerRegistration({ token: "lhrr_abc", expiresAt: "soon" }), null);
});

test("creating a registration token posts to the scoped endpoint with CSRF", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init?: RequestInit } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return Response.json({ token: "lhrr_value", expiresAt: "2026-08-13T13:00:00Z" }, { status: 201 });
  };
  try {
    const result = await createRunnerRegistrationToken({ kind: "organization", organization: "acme" }, "csrf-token");
    assert.equal(result.ok, true);
    if (!result.ok) return;
    assert.match(result.data.token, /^lhrr_/);
    assert.equal(request?.input, "/api/v1/organizations/acme/actions/runners/registration-token");
    assert.equal(request?.init?.method, "POST");
    assert.equal(new Headers(request?.init?.headers).get("X-CSRF-Token"), "csrf-token");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("a token response without the runner prefix is treated as unavailable", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => Response.json({ token: "nope", expiresAt: "2026-08-13T13:00:00Z" }, { status: 201 });
  try {
    const result = await createRunnerRegistrationToken({ kind: "organization", organization: "acme" }, "csrf-token");
    assert.deepEqual(result, { ok: false, kind: "unavailable", code: "invalid_response" });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("revoking a runner encodes the identifier and sends CSRF", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init?: RequestInit } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return new Response(null, { status: 204 });
  };
  try {
    const result = await revokeRunner({ kind: "repository", owner: "acme", repository: "web" }, "runner/id", "csrf");
    assert.equal(result.ok, true);
    assert.equal(request?.input, "/api/v1/repositories/acme/web/actions/runners/runner%2Fid");
    assert.equal(request?.init?.method, "DELETE");
    assert.equal(new Headers(request?.init?.headers).get("X-CSRF-Token"), "csrf");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("runner settings show the configure commands and an inline revoke confirmation", async () => {
  const source = await readFile("src/components/runners/runner-settings.tsx", "utf8");
  assert.match(source, /runnerConfigureCommand\(origin\), runnerRunCommand\(\)/);
  assert.match(source, /organizationWarningBody/);
  assert.match(source, /copy\.revokeConfirmAction/);
  assert.doesNotMatch(source, /window\.confirm/);
  assert.equal(
    runnerConfigureCommand("https://lorehub.example"),
    "lorehub-runner configure --url https://lorehub.example",
  );
});

test("runner settings pages exist for repositories and organizations", async () => {
  const repositoryPage = await readFile("src/app/[locale]/[owner]/[repository]/settings/runners/page.tsx", "utf8");
  const organizationPage = await readFile(
    "src/app/[locale]/organizations/[organization]/settings/runners/page.tsx",
    "utf8",
  );
  assert.match(repositoryPage, /RepositorySettingsTabs/);
  assert.match(repositoryPage, /kind: "repository"/);
  assert.match(organizationPage, /OrganizationSettingsTabs/);
  assert.match(organizationPage, /kind: "organization"/);
});
