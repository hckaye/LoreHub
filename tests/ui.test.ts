import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../", import.meta.url);

test("global shell exposes labelled navigation and search controls", async () => {
  const source = await readText("src/components/layout/site-header.tsx");
  assert.match(source, /aria-label=\{dictionary\.common\.primaryNavigation\}/);
  assert.match(source, /role="search"/);
  assert.match(source, /aria-controls="primary-navigation"/);
});

test("auth UI only renders provider links from the configured provider response", async () => {
  const source = await readText("src/components/auth/auth-page.tsx");
  assert.match(source, /providers\.map/);
  assert.match(source, /providerLoginUrl/);
  assert.match(source, /role="alert"/);
});

test("notification actions have explicit button types and accessible status output", async () => {
  const source = await readText("src/components/notifications/notification-inbox.tsx");
  assert.match(source, /type="button"/);
  assert.match(source, /role="alert"/);
  assert.match(source, /dateTime=\{item\.createdAt\}/);
});

test("user avatars load provider pictures without sending a referrer", async () => {
  const source = await readText("src/components/ui/user-avatar.tsx");
  assert.match(source, /avatarUrl/);
  assert.match(source, /referrerPolicy="no-referrer"/);
});

test("notification settings disable email when the server cannot deliver it", async () => {
  const form = await readText("src/components/account/notification-settings-form.tsx");
  const types = await readText("src/lib/api-types.ts");
  const english = await readText("src/i18n/dictionaries/account-settings.ts");
  const japanese = await readText("src/i18n/dictionaries/account-settings.ts");
  assert.match(form, /disabled=\{!values\.emailAvailable\}/);
  assert.match(form, /dictionary\.accountSettings\.emailUnavailable/);
  assert.match(types, /emailAvailable: boolean/);
  assert.match(english, /Email delivery is not configured for this installation\./);
  assert.match(japanese, /このLoreHubではメール送信が設定されていません。/);
});

async function readText(path: string): Promise<string> {
  return readFile(new URL(path, root), "utf8");
}
