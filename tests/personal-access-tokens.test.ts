import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { createPersonalAccessToken, revokePersonalAccessToken } from "../src/lib/personal-access-token-client";

const token = {
  id: "00000000-0000-4000-8000-000000000001",
  name: "Developer workstation",
  prefix: "lhp_abcdefgh",
  scopes: ["read_api", "write_repository"],
  expiresAt: "2026-09-10T00:00:00Z",
  lastUsedAt: null,
  revokedAt: null,
  createdAt: "2026-08-12T00:00:00Z",
};

test("personal access token creation sends CSRF and accepts the one-time value", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init?: RequestInit } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return Response.json({ token, value: "lhp_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG" }, { status: 201 });
  };
  try {
    const input = {
      name: "Developer workstation",
      scopes: ["read_api", "write_repository"] as const,
      expiresAt: "2026-09-10T00:00:00Z",
    };
    const result = await createPersonalAccessToken({ ...input, scopes: [...input.scopes] }, "csrf-token");
    assert.equal(result.ok, true);
    if (!result.ok) return;
    assert.equal(result.data.token.id, token.id);
    assert.match(result.data.value, /^lhp_/);
    assert.equal(request?.input, "/api/v1/account/personal-access-tokens");
    assert.equal(request?.init?.method, "POST");
    assert.equal(new Headers(request?.init?.headers).get("X-CSRF-Token"), "csrf-token");
    assert.deepEqual(JSON.parse(String(request?.init?.body)), input);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("personal access token client rejects malformed success responses", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => Response.json({ token: { ...token, scopes: ["admin"] }, value: "not-a-token" });
  try {
    const result = await createPersonalAccessToken(
      { name: "Token", scopes: ["read_api"], expiresAt: "2026-09-10T00:00:00Z" },
      "csrf-token",
    );
    assert.deepEqual(result, { ok: false, kind: "unavailable", code: "invalid_response" });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("personal access token revocation encodes the ID and sends CSRF", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init?: RequestInit } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return new Response(null, { status: 204 });
  };
  try {
    const result = await revokePersonalAccessToken("token/id", "csrf-token");
    assert.equal(result.ok, true);
    assert.equal(request?.input, "/api/v1/account/personal-access-tokens/token%2Fid");
    assert.equal(request?.init?.method, "DELETE");
    assert.equal(new Headers(request?.init?.headers).get("X-CSRF-Token"), "csrf-token");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("account settings show one-time token handling and scope controls", async () => {
  const source = await readFile("src/components/account/personal-access-token-settings.tsx", "utf8");
  assert.match(source, /createdValue/);
  assert.match(source, /navigator\.clipboard\.writeText/);
  assert.match(source, /read_repository/);
  assert.match(source, /write_repository/);
  assert.match(source, /revokePersonalAccessToken/);
  assert.match(source, /session\.csrfToken/);
});
