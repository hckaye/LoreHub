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
  assert.match(workflow, /timeout-minutes: 60/);
  assert.match(workflow, /scope=lore-policy-server,ignore-error=true,timeout=20m/);
  assert.match(workflow, /LORE_TEST_SERVER_IMAGE: lorehub\/lore-integration-server:ci/);
  assert.match(workflow, /LORE_TEST_CLIENT_IMAGE: lorehub\/lore-integration-client:ci/);
  assert.match(integration, /docker image inspect "\$LORE_TEST_SERVER_IMAGE"/);
  assert.match(integration, /docker tag "\$LORE_TEST_CLIENT_IMAGE"/);
  assert.match(integration, /build_option=--no-build/);
});
