import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../", import.meta.url);

test("repository settings mount webhook management with authenticated CSRF", async () => {
  const page = await readText("src/app/[locale]/[owner]/[repository]/settings/page.tsx");
  const settings = await readText("src/components/webhooks/repository-webhook-settings.tsx");
  assert.match(page, /RepositoryWebhookSettings/);
  assert.match(page, /session=\{session\}/);
  assert.match(settings, /props\.session\.csrfToken/);
  assert.match(settings, /loadState === "forbidden"/);
  assert.match(settings, /loadState === "unavailable"/);
  assert.match(settings, /listWebhookDeliveries/);
  assert.match(settings, /redeliverWebhook/);
});

test("webhook secret inputs are password fields and are not restored while editing", async () => {
  const form = await readText("src/components/webhooks/repository-webhook-form.tsx");
  const client = await readText("src/lib/webhook-client.ts");
  assert.match(form, /useState\(""\)/);
  assert.match(form, /autoComplete="new-password"/);
  assert.match(form, /type="password"/);
  assert.match(form, /required=\{!editing\}/);
  assert.doesNotMatch(client, /secret: value\.secret/);
});

async function readText(path: string): Promise<string> {
  return readFile(new URL(path, root), "utf8");
}
