import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import en from "../src/i18n/dictionaries/en";
import ja from "../src/i18n/dictionaries/ja";

test("deployment environments are connected to settings and Actions", async () => {
  const [settingsPage, actionsPage, settingsComponent, dashboard] = await Promise.all([
    readFile("src/app/[locale]/[owner]/[repository]/settings/page.tsx", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/actions/page.tsx", "utf8"),
    readFile("src/components/actions/actions-environment-settings.tsx", "utf8"),
    readFile("src/components/repositories/actions-dashboard.tsx", "utf8"),
  ]);
  assert.match(settingsPage, /getActionsEnvironments/);
  assert.match(settingsPage, /ActionsEnvironmentSettings/);
  assert.match(actionsPage, /getDeployments/);
  assert.match(settingsComponent, /session\.csrfToken/);
  assert.match(dashboard, /deployments.*reviews/s);
});

test("deployment protection has paired English and Japanese copy", () => {
  assert.equal(en.actionsEnvironments.deploymentStatuses.pending, "Approval required");
  assert.equal(ja.actionsEnvironments.deploymentStatuses.pending, "承認待ち");
  assert.notEqual(en.actionsEnvironments.description, ja.actionsEnvironments.description);
});

test("deployment state is persisted and excluded from runner claims until released", async () => {
  const [migration, store] = await Promise.all([
    readFile("services/api/migrations/000045_actions_environments.sql", "utf8"),
    readFile("services/api/internal/runner/store.go", "utf8"),
  ]);
  assert.match(migration, /CREATE TABLE repository_environments/);
  assert.match(migration, /CREATE TABLE deployments/);
  assert.match(migration, /status IN \('pending', 'waiting', 'queued'/);
  assert.match(store, /deployment\.status IN \('queued', 'waiting'\)/);
  assert.match(store, /deployment\.wait_until <= now\(\)/);
});
