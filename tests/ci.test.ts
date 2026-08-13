import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("Lore integration reuses the GitHub Actions cache and loaded images", async () => {
  const [workflow, integration] = await Promise.all([
    readFile(".github/workflows/ci.yml", "utf8"),
    readFile("scripts/lore-merge-integration.sh", "utf8"),
  ]);
  assert.match(workflow, /name: Build Lore Server integration image[\s\S]*load: true/);
  assert.match(workflow, /name: Build Lore client integration image[\s\S]*load: true/);
  assert.match(workflow, /scope=lore-policy-server/);
  assert.match(workflow, /scope=lore-policy-client/);
  assert.match(workflow, /timeout-minutes: 75/);
  assert.match(workflow, /scope=lore-policy-server,ignore-error=true,timeout=20m/);
  assert.match(workflow, /LORE_TEST_SERVER_IMAGE: lorehub\/lore-integration-server:ci/);
  assert.match(workflow, /LORE_TEST_CLIENT_IMAGE: lorehub\/lore-integration-client:ci/);
  assert.match(integration, /docker image inspect "\$LORE_TEST_SERVER_IMAGE"/);
  assert.match(integration, /docker tag "\$LORE_TEST_CLIENT_IMAGE"/);
  assert.match(integration, /build_option=--no-build/);
});

test("CI validates the CLI, clean Compose setup, and browser flow", async () => {
  const [workflow, packageFile, browserTest] = await Promise.all([
    readFile(".github/workflows/ci.yml", "utf8"),
    readFile("package.json", "utf8"),
    readFile("e2e/lorehub.spec.ts", "utf8"),
  ]);
  assert.match(workflow, /npm run cli:test/);
  assert.match(workflow, /npm run compose:test/);
  assert.match(workflow, /browser-e2e:/);
  assert.match(workflow, /npm run e2e/);
  assert.match(packageFile, /"e2e": "playwright test"/);
  assert.match(browserTest, /Create Lore repository/);
  assert.match(browserTest, /Create issue/);
});
