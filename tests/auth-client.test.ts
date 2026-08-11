import assert from "node:assert/strict";
import test from "node:test";

import {
  classifyMutationStatus,
  deleteJson,
  deleteJsonWithBody,
  patchJson,
  postJson,
  putJson,
} from "../src/lib/auth-client";

test("mutation status maps authentication and API failures", () => {
  assert.equal(classifyMutationStatus(401), "unauthorized");
  assert.equal(classifyMutationStatus(403), "forbidden");
  assert.equal(classifyMutationStatus(409), "conflict");
  assert.equal(classifyMutationStatus(412), "conflict");
  assert.equal(classifyMutationStatus(422), "invalid");
  assert.equal(classifyMutationStatus(503), "unavailable");
});

test("PATCH sends an optimistic concurrency header", async () => {
  const originalFetch = globalThis.fetch;
  let headers = new Headers();
  globalThis.fetch = async (_input, init) => {
    headers = new Headers(init?.headers);
    return Response.json({ id: "issue-1" });
  };
  try {
    const result = await patchJson<{ id: string }>("/api/v1/issues/1", { state: "closed" }, "csrf-token", {
      "If-Match": '"2026-08-11T00:00:00Z"',
    });
    assert.equal(result.ok, true);
    assert.equal(headers.get("If-Match"), '"2026-08-11T00:00:00Z"');
    assert.equal(headers.get("X-CSRF-Token"), "csrf-token");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("PUT and 204 DELETE mutations preserve CSRF and accept empty responses", async () => {
  const originalFetch = globalThis.fetch;
  const methods: string[] = [];
  const csrfHeaders: Array<string | null> = [];
  globalThis.fetch = async (_input, init) => {
    methods.push(init?.method ?? "");
    csrfHeaders.push(new Headers(init?.headers).get("X-CSRF-Token"));
    return methods.length === 1 ? Response.json({ id: "label-1" }) : new Response(null, { status: 204 });
  };
  try {
    const applied = await putJson<{ id: string }>("/api/v1/issues/1/labels/1", undefined, "csrf-token");
    const removed = await deleteJson<null>("/api/v1/issues/1/labels/1", "csrf-token");
    assert.deepEqual(applied, { ok: true, data: { id: "label-1" } });
    assert.deepEqual(removed, { ok: true, data: null });
    assert.deepEqual(methods, ["PUT", "DELETE"]);
    assert.deepEqual(csrfHeaders, ["csrf-token", "csrf-token"]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("JSON mutations use same-origin credentials and CSRF header", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init: RequestInit | undefined } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return new Response(JSON.stringify({ id: "issue-1" }), {
      headers: { "Content-Type": "application/json" },
      status: 201,
    });
  };
  try {
    const result = await postJson<{ id: string }>("/api/v1/issues", { title: "Test" }, "csrf-token");
    assert.deepEqual(result, { ok: true, data: { id: "issue-1" } });
    assert.equal(request?.init?.credentials, "include");
    const headers = new Headers(request?.init?.headers);
    assert.equal(headers.get("X-CSRF-Token"), "csrf-token");
    assert.equal(headers.get("Content-Type"), "application/json");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("DELETE can send an optimistic version as strict JSON", async () => {
  const originalFetch = globalThis.fetch;
  let request: RequestInit | undefined;
  globalThis.fetch = async (_input, init) => {
    request = init;
    return new Response(null, { status: 204 });
  };
  try {
    const result = await deleteJsonWithBody<null>("/api/v1/releases/1", { expectedVersion: 7 }, "csrf-token");
    assert.deepEqual(result, { ok: true, data: null });
    assert.equal(request?.method, "DELETE");
    assert.equal(new Headers(request?.headers).get("Content-Type"), "application/json");
    assert.equal(request?.body, JSON.stringify({ expectedVersion: 7 }));
  } finally {
    globalThis.fetch = originalFetch;
  }
});
