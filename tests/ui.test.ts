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

async function readText(path: string): Promise<string> {
  return readFile(new URL(path, root), "utf8");
}
