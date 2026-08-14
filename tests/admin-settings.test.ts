import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  adminSettingsInput,
  hostedLoreServerChoice,
  hostedLoreServerOverride,
  normalizeAdminSettings,
} from "../src/lib/admin-settings";
import { updateAdminSettings } from "../src/lib/admin-settings-client";

const settings = {
  hostedLoreServerEnabled: true,
  hostedLoreServerOverride: null,
  hostedLoreServerDefault: true,
};

test("admin settings map radio choices onto the override field", () => {
  assert.equal(hostedLoreServerChoice(null), "default");
  assert.equal(hostedLoreServerChoice(true), "enabled");
  assert.equal(hostedLoreServerChoice(false), "disabled");
  assert.equal(hostedLoreServerOverride("default"), null);
  assert.equal(hostedLoreServerOverride("enabled"), true);
  assert.equal(hostedLoreServerOverride("disabled"), false);
  assert.deepEqual(adminSettingsInput(null), { hostedLoreServerOverride: null });
});

test("admin settings payloads require booleans and a nullable override", () => {
  assert.deepEqual(normalizeAdminSettings(settings), settings);
  assert.deepEqual(normalizeAdminSettings({ ...settings, hostedLoreServerOverride: false }), {
    ...settings,
    hostedLoreServerOverride: false,
  });
  assert.equal(normalizeAdminSettings({ ...settings, hostedLoreServerOverride: "true" }), null);
  assert.equal(normalizeAdminSettings({ ...settings, hostedLoreServerEnabled: "yes" }), null);
  assert.equal(normalizeAdminSettings({ hostedLoreServerEnabled: true }), null);
});

test("saving instance settings puts the admin override with CSRF", async () => {
  const originalFetch = globalThis.fetch;
  let request: { input: RequestInfo | URL; init?: RequestInit } | undefined;
  globalThis.fetch = async (input, init) => {
    request = { input, init };
    return Response.json({ ...settings, hostedLoreServerOverride: false, hostedLoreServerEnabled: false });
  };
  try {
    const result = await updateAdminSettings(false, "csrf");
    assert.equal(result.ok, true);
    assert.equal(request?.input, "/api/v1/admin/settings");
    assert.equal(request?.init?.method, "PUT");
    assert.equal(new Headers(request?.init?.headers).get("X-CSRF-Token"), "csrf");
    assert.deepEqual(JSON.parse(String(request?.init?.body)), { hostedLoreServerOverride: false });
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("clearing the hosted Lore server override sends JSON null", async () => {
  const originalFetch = globalThis.fetch;
  let body = "";
  globalThis.fetch = async (_input, init) => {
    body = String(init?.body);
    return Response.json(settings);
  };
  try {
    const result = await updateAdminSettings(null, "csrf");
    assert.equal(result.ok, true);
    assert.equal(body, '{"hostedLoreServerOverride":null}');
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("a forbidden admin settings API leaves the instance page with an empty state", async () => {
  const page = await readFile("src/app/[locale]/settings/_pages/instance.tsx", "utf8");
  assert.match(page, /getAdminSettings/);
  assert.match(page, /reason === "forbidden"/);
  assert.match(page, /forbiddenTitle/);
  const layout = await readFile("src/app/[locale]/settings/layout.tsx", "utf8");
  assert.match(layout, /getAdminSettings/);
  assert.match(layout, /showInstanceSettings=\{adminSettings\.ok\}/);
});

test("the instance settings surface offers GitHub radio options and a primary save", async () => {
  const source = await readFile("src/components/admin/instance-settings.tsx", "utf8");
  assert.match(source, /updateAdminSettings/);
  assert.match(source, /type="radio"/);
  assert.match(source, /mutationFailureMessage/);
  assert.match(source, /styles\.primaryButton/);
  assert.match(source, /settings-form\.module\.css/);
});
