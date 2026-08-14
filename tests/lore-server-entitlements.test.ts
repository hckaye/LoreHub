import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import type { Dictionary } from "../src/i18n";
import en from "../src/i18n/dictionaries/en";
import ja from "../src/i18n/dictionaries/ja";
import { mutationFailureMessage } from "../src/lib/mutation-messages";

test("repository creation explains that the organization has no Lore Server", () => {
  for (const dictionary of [en, ja] as unknown as Dictionary[]) {
    for (const code of ["hosted_lore_server_entitlement_required", "no_lore_server_available"]) {
      assert.equal(mutationFailureMessage("conflict", dictionary, code), dictionary.errors.noLoreServer);
    }
    for (const code of ["default_server_unavailable", "explicit_server_unavailable"]) {
      assert.equal(mutationFailureMessage("conflict", dictionary, code), dictionary.errors.loreServerUnavailable);
    }
    assert.equal(mutationFailureMessage("conflict", dictionary, "conflict"), dictionary.errors.conflict);
    assert.equal(mutationFailureMessage("forbidden", dictionary), dictionary.errors.forbidden);
  }
});

test("the repository form passes the API reason to the failure message", async () => {
  const source = await readFile("src/components/repositories/register-repository-form.tsx", "utf8");
  assert.match(source, /mutationFailureMessage\(result\.kind, dictionary, result\.code\)/);
});

test("new organizations receive the entitlements the installation configures", async () => {
  const [store, config, compose, example] = await Promise.all([
    readFile("services/api/internal/platform/store.go", "utf8"),
    readFile("services/api/internal/config/default_entitlements.go", "utf8"),
    readFile("infra/compose.yaml", "utf8"),
    readFile(".env.example", "utf8"),
  ]);
  assert.match(store, /INSERT INTO entitlements \(organization_id, feature, grant_source\)/);
  assert.match(store, /VALUES \(\$1, \$2, 'default'\)/);
  assert.match(config, /LOREHUB_DEFAULT_ORGANIZATION_ENTITLEMENTS contains unknown feature/);
  assert.match(compose, /LOREHUB_DEFAULT_ORGANIZATION_ENTITLEMENTS/);
  assert.match(compose, /hosted_lore_server,hosted_runners/);
  assert.match(example, /LOREHUB_DEFAULT_ORGANIZATION_ENTITLEMENTS=hosted_lore_server,hosted_runners/);
  assert.match(example, /LOREHUB_INSTANCE_ADMIN_USERNAMES=/);
});
