import assert from "node:assert/strict";
import test from "node:test";

import { localePathFrom, loginUrl, repositoryPath, safeReturnTo } from "../src/lib/routes";

test("repository routes keep the locale and encode path segments", () => {
  assert.equal(repositoryPath("ja", "Epic Games", "Lore Hub", "pulls"), "/ja/Epic%20Games/Lore%20Hub/pulls");
  assert.equal(repositoryPath("en", "owner", "repository"), "/en/owner/repository");
});

test("return paths accept only same-origin relative paths", () => {
  assert.equal(safeReturnTo("/ja/owner/repo/issues?state=open"), "/ja/owner/repo/issues?state=open");
  assert.equal(safeReturnTo("https://example.com/steal", "/en"), "/en");
  assert.equal(safeReturnTo("//example.com/steal", "/en"), "/en");
  assert.equal(safeReturnTo("/\\example.com", "/en"), "/en");
});

test("login URLs use relative return_to values and the Keycloak registration prompt", () => {
  const signIn = loginUrl("/ja/owner/repo/issues");
  const signUp = loginUrl("/ja/owner/repo/issues", true);
  assert.equal(signIn, "/auth/login?return_to=%2Fja%2Fowner%2Frepo%2Fissues");
  assert.equal(signUp, "/auth/login?return_to=%2Fja%2Fowner%2Frepo%2Fissues&prompt=create");
  assert.ok(!signIn.includes("screen_hint"));
});

test("locale switching preserves the route shape", () => {
  assert.equal(localePathFrom("/en/owner/repository/issues", "ja"), "/ja/owner/repository/issues");
});
