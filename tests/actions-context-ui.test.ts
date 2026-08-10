import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../", import.meta.url);

test("Actions settings UI never refills a secret and sends the session CSRF token", async () => {
  const source = await readText("src/components/actions/actions-context-settings.tsx");
  const form = await readText("src/components/actions/actions-context-entry-form.tsx");
  assert.match(source, /setValue\(entry\.secret \? ""/);
  assert.match(source, /session\.csrfToken/);
  assert.match(source, /loadState === "forbidden"/);
  assert.match(source, /loadState === "unavailable"/);
  assert.match(form, /autoComplete="new-password"/);
  assert.match(form, /type="password"/);
});

test("repository and organization settings mount the scoped Actions settings UI", async () => {
  const repositoryPage = await readText("src/app/[locale]/[owner]/[repository]/settings/page.tsx");
  const organizationPage = await readText("src/app/[locale]/organizations/[organization]/settings/page.tsx");
  assert.match(repositoryPage, /kind: "repository"/);
  assert.match(organizationPage, /kind: "organization"/);
  assert.match(repositoryPage, /ActionsContextSettings/);
  assert.match(organizationPage, /ActionsContextSettings/);
});

async function readText(path: string): Promise<string> {
  return readFile(new URL(path, root), "utf8");
}
