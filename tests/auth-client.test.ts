import assert from "node:assert/strict";
import test from "node:test";

import { classifyMutationStatus, postJson } from "../src/lib/auth-client";

test("mutation status maps authentication and API failures", () => {
  assert.equal(classifyMutationStatus(401), "unauthorized");
  assert.equal(classifyMutationStatus(403), "forbidden");
  assert.equal(classifyMutationStatus(409), "conflict");
  assert.equal(classifyMutationStatus(422), "invalid");
  assert.equal(classifyMutationStatus(503), "unavailable");
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
