import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  createLoreServerRegistrationToken,
  revokeLoreServer,
  setDefaultLoreServer,
} from "../src/lib/lore-server-client";
import {
  loreServerConfigureCommand,
  loreServerStatus,
  normalizeDefaultLoreServer,
  normalizeLoreServerList,
  normalizeLoreServerRegistration,
  type LoreServer,
} from "../src/lib/lore-servers";

const now = new Date("2026-08-13T12:00:00Z");

const server: LoreServer = {
  id: "00000000-0000-4000-8000-000000000020",
  instanceScope: false,
  organizationId: "00000000-0000-4000-8000-000000000001",
  name: "tokyo-lore-1",
  publicUrl: "lores://lore.acme.example",
  status: "active",
  credentialExpiresAt: "2027-08-13T12:00:00Z",
  lastSeenAt: "2026-08-13T11:58:00Z",
  loreBuildVersion: "0.9.2",
  revokedAt: null,
  createdAt: "2026-07-01T09:00:00Z",
};

test("Lore server status uses the heartbeat and revocation state", () => {
  assert.equal(loreServerStatus(server, now), "active");
  assert.equal(loreServerStatus({ ...server, lastSeenAt: "2026-08-13T11:30:00Z" }, now), "offline");
  assert.equal(loreServerStatus({ ...server, lastSeenAt: null }, now), "offline");
  assert.equal(loreServerStatus({ ...server, status: "revoked" }, now), "revoked");
});

test("Lore server payloads are normalized and malformed records are rejected", () => {
  const servers = normalizeLoreServerList({ servers: [server, { ...server, id: "b", instanceScope: true }] });
  assert.equal(servers?.length, 2);
  assert.equal(servers?.[1].instanceScope, true);
  assert.equal(normalizeLoreServerList({ servers: [{ ...server, publicUrl: 7 }] }), null);
  assert.equal(normalizeDefaultLoreServer({ server: null }), null);
  assert.equal(normalizeDefaultLoreServer({ server })?.id, server.id);
});

test("Lore server registration responses expose the one-time lhsr_ token", () => {
  const registration = normalizeLoreServerRegistration({
    token: { id: "t1", expiresAt: "2026-08-13T13:00:00Z" },
    value: "lhsr_secret",
  });
  assert.deepEqual(registration, { token: "lhsr_secret", expiresAt: "2026-08-13T13:00:00Z" });
  assert.equal(
    normalizeLoreServerRegistration({ token: { expiresAt: "2026-08-13T13:00:00Z" }, value: "lhrr_x" }),
    null,
  );
});

test("registering a Lore server posts to the organization endpoint with CSRF", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init?: RequestInit } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return Response.json({ token: { expiresAt: "2026-08-13T13:00:00Z" }, value: "lhsr_secret" }, { status: 201 });
  };
  try {
    const result = await createLoreServerRegistrationToken("acme", "csrf");
    assert.equal(result.ok, true);
    assert.equal(request?.input, "/api/v1/organizations/acme/lore-servers/registration-tokens");
    assert.equal(request?.init?.method, "POST");
    assert.equal(new Headers(request?.init?.headers).get("X-CSRF-Token"), "csrf");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("revoking a Lore server deletes the scoped resource", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init?: RequestInit } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return new Response(null, { status: 204 });
  };
  try {
    const result = await revokeLoreServer("acme", "server/id", "csrf");
    assert.equal(result.ok, true);
    assert.equal(request?.input, "/api/v1/organizations/acme/lore-servers/server%2Fid");
    assert.equal(request?.init?.method, "DELETE");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("the default server selection sends the identifier and accepts a cleared default", async () => {
  const originalFetch = globalThis.fetch;
  const requests: { input: RequestInfo | URL; init?: RequestInit }[] = [];
  globalThis.fetch = async (input, init) => {
    requests.push({ input, init });
    return Response.json({ server: requests.length === 1 ? server : null });
  };
  try {
    const selected = await setDefaultLoreServer("acme", server.id, "csrf");
    assert.equal(selected.ok && selected.data?.id, server.id);
    const cleared = await setDefaultLoreServer("acme", null, "csrf");
    assert.equal(cleared.ok && cleared.data, null);
    assert.equal(requests[0]?.input, "/api/v1/organizations/acme/lore-servers/default");
    assert.equal(requests[0]?.init?.method, "PUT");
    assert.deepEqual(JSON.parse(String(requests[0]?.init?.body)), { loreServerId: server.id });
    assert.deepEqual(JSON.parse(String(requests[1]?.init?.body)), { loreServerId: null });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("the Lore server page shows the agent command and the default server form", async () => {
  const settings = await readFile("src/components/lore-servers/lore-server-settings.tsx", "utf8");
  const defaultForm = await readFile("src/components/lore-servers/default-lore-server-form.tsx", "utf8");
  const page = await readFile("src/app/[locale]/organizations/[organization]/settings/lore-servers/page.tsx", "utf8");
  assert.match(settings, /loreServerConfigureCommand\(origin\), loreServerRunCommand\(\)/);
  assert.match(settings, /DefaultLoreServerForm/);
  assert.match(defaultForm, /setDefaultLoreServer/);
  assert.match(defaultForm, /hosted_lore_server_entitlement_required/);
  assert.match(page, /OrganizationSettingsTabs/);
  assert.equal(
    loreServerConfigureCommand("https://lorehub.example"),
    "lorehub-lores-agent configure --url https://lorehub.example",
  );
});
