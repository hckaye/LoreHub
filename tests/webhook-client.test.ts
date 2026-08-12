import assert from "node:assert/strict";
import test from "node:test";

import {
  classifyWebhookStatus,
  createRepositoryWebhook,
  deleteRepositoryWebhook,
  listRepositoryWebhooks,
  listWebhookDeliveries,
  redeliverWebhook,
  updateRepositoryWebhook,
  webhookBasePath,
} from "../src/lib/webhook-client";

const webhook = {
  id: "webhook-1",
  url: "https://hooks.example.test/lorehub",
  events: ["issues", "pull_requests"],
  active: true,
  secretConfigured: true,
  secret: "must-not-leak",
  createdAt: "2026-08-12T00:00:00Z",
  updatedAt: "2026-08-12T00:00:00Z",
};

const delivery = {
  id: "delivery-1",
  event: "issues",
  status: "succeeded",
  attemptCount: 1,
  responseStatus: 204,
  responseBody: "",
  lastError: "",
  deliveredAt: "2026-08-12T00:00:01Z",
  createdAt: "2026-08-12T00:00:00Z",
  updatedAt: "2026-08-12T00:00:01Z",
};

test("webhook paths encode repository and delivery identifiers", () => {
  assert.equal(
    webhookBasePath("acme studio", "game/client"),
    "/api/v1/repositories/acme%20studio/game%2Fclient/webhooks",
  );
});

test("webhook list validates event names and never copies a returned secret", async () => {
  const restore = stubFetch(() =>
    Response.json({
      webhooks: [webhook],
      availableEvents: ["discussions", "issues", "reviews", "statuses", "wiki"],
    }),
  );
  try {
    const result = await listRepositoryWebhooks("acme", "game");
    assert.equal(result.ok, true);
    if (!result.ok) return;
    assert.equal("secret" in result.data.webhooks[0], false);
    assert.deepEqual(result.data.availableEvents, ["discussions", "issues", "reviews", "statuses", "wiki"]);
  } finally {
    restore();
  }

  const restoreInvalid = stubFetch(() =>
    Response.json({ webhooks: [webhook], availableEvents: ["issues", "unsupported"] }),
  );
  try {
    const result = await listRepositoryWebhooks("acme", "game");
    assert.deepEqual(result, { ok: false, kind: "unavailable", code: "invalid_response" });
  } finally {
    restoreInvalid();
  }
});

test("webhook mutations preserve CSRF and accept empty success bodies", async () => {
  const requests: Array<{ input: RequestInfo | URL; init?: RequestInit }> = [];
  const restore = stubFetch((input, init) => {
    requests.push({ input, init });
    if (init?.method === "DELETE") return new Response(null, { status: 204 });
    if (String(input).endsWith("/redeliver")) return new Response(null, { status: 202 });
    return Response.json(webhook, { status: init?.method === "POST" ? 201 : 200 });
  });
  try {
    const input = {
      url: webhook.url,
      events: ["issues" as const],
      active: true,
      secret: "sixteen-characters",
    };
    assert.equal((await createRepositoryWebhook("acme", "game", input, "csrf-token")).ok, true);
    assert.equal((await updateRepositoryWebhook("acme", "game", "hook/id", input, "csrf-token")).ok, true);
    assert.equal((await deleteRepositoryWebhook("acme", "game", "hook/id", "csrf-token")).ok, true);
    assert.equal((await redeliverWebhook("acme", "game", "hook/id", "delivery/id", "csrf-token")).ok, true);
    assert.equal(requests.length, 4);
    for (const request of requests) {
      assert.equal(request.init?.credentials, "include");
      assert.equal(new Headers(request.init?.headers).get("X-CSRF-Token"), "csrf-token");
    }
    assert.equal(String(requests[1].input), "/api/v1/repositories/acme/game/webhooks/hook%2Fid");
    assert.equal(
      String(requests[3].input),
      "/api/v1/repositories/acme/game/webhooks/hook%2Fid/deliveries/delivery%2Fid/redeliver",
    );
  } finally {
    restore();
  }
});

test("webhook delivery list rejects malformed records", async () => {
  const restore = stubFetch(() => Response.json({ deliveries: [delivery] }));
  try {
    const result = await listWebhookDeliveries("acme", "game", "hook");
    assert.equal(result.ok, true);
    if (result.ok) assert.deepEqual(result.data, [delivery]);
  } finally {
    restore();
  }

  const restoreInvalid = stubFetch(() => Response.json({ deliveries: [{ ...delivery, status: "unknown" }] }));
  try {
    const result = await listWebhookDeliveries("acme", "game", "hook");
    assert.deepEqual(result, { ok: false, kind: "unavailable", code: "invalid_response" });
  } finally {
    restoreInvalid();
  }
});

test("webhook status classification distinguishes permissions and input failures", () => {
  assert.equal(classifyWebhookStatus(401), "unauthorized");
  assert.equal(classifyWebhookStatus(403), "forbidden");
  assert.equal(classifyWebhookStatus(404), "not-found");
  assert.equal(classifyWebhookStatus(422), "invalid");
  assert.equal(classifyWebhookStatus(409), "conflict");
  assert.equal(classifyWebhookStatus(503), "unavailable");
});

function stubFetch(
  implementation: (input: RequestInfo | URL, init?: RequestInit) => Response | Promise<Response>,
): () => void {
  const original = globalThis.fetch;
  globalThis.fetch = implementation as typeof fetch;
  return () => {
    globalThis.fetch = original;
  };
}
