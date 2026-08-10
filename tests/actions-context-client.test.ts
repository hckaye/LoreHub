import assert from "node:assert/strict";
import test from "node:test";

import {
  actionsContextEntryPath,
  actionsContextListPath,
  classifyActionsContextStatus,
  deleteActionsContextEntry,
  listActionsContextEntries,
  putActionsContextEntry,
} from "../src/lib/actions-context-client";

const metadata = {
  id: "entry-1",
  scope: {
    kind: "environment",
    organizationId: "organization-1",
    repositoryId: "repository-1",
    environment: "production/us",
  },
  name: "DEPLOY_TOKEN",
  secret: true,
  keyId: "primary",
  updatedBy: "user-1",
  createdAt: "2026-08-10T00:00:00Z",
  updatedAt: "2026-08-10T00:00:00Z",
};

test("Actions context paths encode repository, environment, and entry names", () => {
  const location = {
    kind: "environment" as const,
    owner: "acme studio",
    repository: "game/client",
    environment: "production/us",
  };
  assert.equal(
    actionsContextListPath(location),
    "/api/v1/repositories/acme%20studio/game%2Fclient/actions/settings" +
      "?scopeKind=environment&environment=production%2Fus",
  );
  assert.equal(
    actionsContextEntryPath(location, "secret", "DEPLOY TOKEN"),
    "/api/v1/repositories/acme%20studio/game%2Fclient/actions/settings/environment/secret/DEPLOY%20TOKEN" +
      "?environment=production%2Fus",
  );
});

test("secret values are discarded from Actions settings list responses", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init: RequestInit | undefined } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return Response.json({ entries: [{ ...metadata, value: "must-not-leak" }] });
  };
  try {
    const result = await listActionsContextEntries("/api/v1/organizations/acme/actions/settings");
    assert.equal(result.ok, true);
    if (!result.ok) return;
    assert.equal("value" in result.data[0], false);
    assert.equal(request?.init?.credentials, "include");
    assert.equal(new Headers(request?.init?.headers).get("Accept"), "application/json");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("Actions settings mutations send CSRF and accept delete without a response body", async () => {
  const originalFetch = globalThis.fetch;
  const requests: RequestInit[] = [];
  globalThis.fetch = async (_input, init) => {
    requests.push(init ?? {});
    if (init?.method === "DELETE") return new Response(null, { status: 204 });
    return Response.json({ ...metadata, secret: false, name: "BUILD_MODE", value: "release" });
  };
  try {
    const putResult = await putActionsContextEntry("/settings/variable/BUILD_MODE", "release", "csrf-token");
    const deleteResult = await deleteActionsContextEntry("/settings/variable/BUILD_MODE", "csrf-token");
    assert.equal(putResult.ok, true);
    assert.equal(deleteResult.ok, true);
    assert.equal(requests[0]?.credentials, "include");
    assert.equal(new Headers(requests[0]?.headers).get("X-CSRF-Token"), "csrf-token");
    assert.equal(requests[0]?.body, JSON.stringify({ value: "release" }));
    assert.equal(requests[1]?.method, "DELETE");
    assert.equal(new Headers(requests[1]?.headers).get("X-CSRF-Token"), "csrf-token");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("Actions settings status classification preserves permission and availability failures", () => {
  assert.equal(classifyActionsContextStatus(401), "unauthorized");
  assert.equal(classifyActionsContextStatus(403), "forbidden");
  assert.equal(classifyActionsContextStatus(404), "not-found");
  assert.equal(classifyActionsContextStatus(400), "invalid");
  assert.equal(classifyActionsContextStatus(503), "unavailable");
});
