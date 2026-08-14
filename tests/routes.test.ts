import assert from "node:assert/strict";
import test from "node:test";

import {
  accountSettingsPath,
  accountSettingsSection,
  brandedAuthUrl,
  localePathFrom,
  loginUrl,
  providerLoginUrl,
  repositoryBranchesPath,
  repositoryPath,
  safeReturnTo,
} from "../src/lib/routes";

test("repository routes keep the locale and encode path segments", () => {
  assert.equal(repositoryPath("ja", "Epic Games", "Lore Hub", "pulls"), "/ja/Epic%20Games/Lore%20Hub/pulls");
  assert.equal(repositoryPath("en", "owner", "repository"), "/en/owner/repository");
  assert.equal(repositoryPath("ja", "owner", "repository", "releases"), "/ja/owner/repository/releases");
  assert.equal(repositoryPath("en", "owner", "repository", "wiki"), "/en/owner/repository/wiki");
  assert.equal(repositoryBranchesPath("ja", "Epic Games", "Lore Hub"), "/ja/Epic%20Games/Lore%20Hub/branches");
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

test("branded auth pages and provider hints keep the return path relative", () => {
  assert.equal(brandedAuthUrl("ja", "/ja/settings"), "/ja/auth/login?return_to=%2Fja%2Fsettings");
  assert.equal(
    providerLoginUrl("/ja/settings", "github", true),
    "/auth/login?return_to=%2Fja%2Fsettings&provider=github&prompt=create",
  );
  assert.equal(providerLoginUrl("https://example.com", "google"), "/auth/login?return_to=%2F&provider=google");
});

test("locale switching preserves the route shape", () => {
  assert.equal(localePathFrom("/en/owner/repository/issues", "ja"), "/ja/owner/repository/issues");
});

test("account settings paths follow GitHub's settings URLs", () => {
  assert.equal(accountSettingsPath("en"), "/en/settings/profile");
  assert.equal(accountSettingsPath("ja", "tokens/new"), "/ja/settings/tokens/new");
  assert.equal(accountSettingsSection("/en/settings/tokens/new"), "tokens/new");
  assert.equal(accountSettingsSection("/ja/settings/profile"), "profile");
});
