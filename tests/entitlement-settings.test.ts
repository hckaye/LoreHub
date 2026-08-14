import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { grantEntitlement, revokeEntitlement } from "../src/lib/entitlement-client";
import {
  entitlementInput,
  isEntitlementFeature,
  isEntitlementSubjectID,
  normalizeEntitlementList,
} from "../src/lib/entitlements";

const organizationID = "00000000-0000-4000-8000-000000000001";
const userID = "00000000-0000-4000-8000-000000000002";

const entitlement = {
  organizationId: organizationID,
  userId: null,
  feature: "hosted_runners",
  grantedBy: "00000000-0000-4000-8000-000000000003",
  grantSource: "manual",
  createdAt: "2026-08-01T09:00:00Z",
  revokedAt: null,
};

test("entitlement subjects must be identifiers, not slugs or usernames", () => {
  assert.equal(isEntitlementSubjectID(organizationID), true);
  assert.equal(isEntitlementSubjectID("acme"), false);
  assert.equal(isEntitlementFeature("hosted_lore_server"), true);
  assert.equal(isEntitlementFeature("hosted_something"), false);
});

test("the grant payload names either the organization or the user", () => {
  assert.deepEqual(entitlementInput({ kind: "organization", id: organizationID }, "hosted_runners"), {
    organizationId: organizationID,
    feature: "hosted_runners",
  });
  assert.deepEqual(entitlementInput({ kind: "user", id: userID }, "hosted_lore_server"), {
    userId: userID,
    feature: "hosted_lore_server",
  });
});

test("entitlement lists are normalized and subject-less records are rejected", () => {
  const entitlements = normalizeEntitlementList({ entitlements: [entitlement] });
  assert.equal(entitlements?.length, 1);
  assert.equal(entitlements?.[0].grantSource, "manual");
  assert.equal(normalizeEntitlementList({ entitlements: [{ ...entitlement, organizationId: null }] }), null);
});

test("granting an entitlement posts the admin payload with CSRF", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init?: RequestInit } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return Response.json(entitlement, { status: 201 });
  };
  try {
    const result = await grantEntitlement({ kind: "organization", id: organizationID }, "hosted_runners", "csrf");
    assert.equal(result.ok, true);
    assert.equal(request?.input, "/api/v1/admin/entitlements");
    assert.equal(request?.init?.method, "POST");
    assert.equal(new Headers(request?.init?.headers).get("X-CSRF-Token"), "csrf");
    assert.deepEqual(JSON.parse(String(request?.init?.body)), {
      organizationId: organizationID,
      feature: "hosted_runners",
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("revoking an entitlement sends the subject in the DELETE body", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init?: RequestInit } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return new Response(null, { status: 204 });
  };
  try {
    const result = await revokeEntitlement({ kind: "user", id: userID }, "hosted_lore_server", "csrf");
    assert.equal(result.ok, true);
    assert.equal(request?.init?.method, "DELETE");
    assert.deepEqual(JSON.parse(String(request?.init?.body)), {
      userId: userID,
      feature: "hosted_lore_server",
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("a forbidden admin API leaves the entitlement page with an empty state", async () => {
  const page = await readFile("src/app/[locale]/settings/_pages/entitlements.tsx", "utf8");
  assert.match(page, /getEntitlements/);
  assert.match(page, /reason === "forbidden"/);
  assert.match(page, /forbiddenTitle/);
  const layout = await readFile("src/app/[locale]/settings/layout.tsx", "utf8");
  assert.match(layout, /getEntitlements/);
  assert.match(layout, /showEntitlements=\{entitlements\.ok\}/);
});

test("the entitlement admin surface offers grant and revoke controls", async () => {
  const source = await readFile("src/components/admin/entitlement-settings.tsx", "utf8");
  assert.match(source, /grantEntitlement/);
  assert.match(source, /revokeEntitlement/);
  assert.match(source, /copy\.revokeConfirmAction/);
  assert.doesNotMatch(source, /window\.confirm/);
});
