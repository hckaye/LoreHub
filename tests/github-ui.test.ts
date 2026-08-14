import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("repository pages use GitHub's header, primary tabs, code layout, and About column", async () => {
  const [header, headerStyles, page, browser, about] = await Promise.all([
    readFile("src/components/repositories/repository-header.tsx", "utf8"),
    readFile("src/components/repositories/repository-header.module.css", "utf8"),
    readFile("src/app/[locale]/[owner]/[repository]/page.tsx", "utf8"),
    readFile("src/components/repositories/code-browser.tsx", "utf8"),
    readFile("src/components/repositories/repository-about.tsx", "utf8"),
  ]);
  assert.match(header, /\["code", Code2\]/);
  assert.match(header, /\["issues", CircleDot\]/);
  assert.match(header, /\["pulls", GitPullRequest\]/);
  assert.match(header, /RepositoryMoreMenu/);
  assert.match(headerStyles, /--nav-active-border/);
  assert.match(page, /className=\{styles\.codeColumn\}/);
  assert.match(page, /<RepositoryAbout/);
  assert.match(browser, /className=\{styles\.revisionBar\}/);
  assert.match(browser, /className=\{styles\.tableWrap\}/);
  assert.match(about, /repository\.homepageUrl/);
  assert.match(about, /repository\.starCount/);
  assert.match(about, /repository\.watcherCount/);
});

test("global navigation and sign-in follow GitHub's compact shells", async () => {
  const [siteHeader, siteStyles, authPage, authStyles, globalStyles, dashboard] = await Promise.all([
    readFile("src/components/layout/site-header.tsx", "utf8"),
    readFile("src/components/layout/site-header.module.css", "utf8"),
    readFile("src/components/auth/auth-page.tsx", "utf8"),
    readFile("src/components/auth/auth-page.module.css", "utf8"),
    readFile("src/app/[locale]/globals.css", "utf8"),
    readFile("src/components/dashboard/dashboard.tsx", "utf8"),
  ]);
  assert.match(siteHeader, /className=\{styles\.iconAction\}/);
  assert.match(siteHeader, /<CreateMenu compact/);
  assert.match(siteStyles, /width: 100%/);
  assert.match(authPage, /data-auth-page/);
  assert.match(authStyles, /width: min\(340px, 100%\)/);
  assert.match(globalStyles, /body:has\(\[data-auth-page\]\)/);
  assert.match(dashboard, /className=\{styles\.sidebar\}/);
  assert.match(dashboard, /className=\{styles\.main\}/);
  assert.match(dashboard, /className=\{styles\.explore\}/);
});

test("account settings use GitHub's sidebar, split pages, and developer token flow", async () => {
  const [layout, index, nav, styles, profile, notifications, repositories, tokens, newToken, menu] = await Promise.all([
    readFile("src/app/[locale]/settings/layout.tsx", "utf8"),
    readFile("src/app/[locale]/settings/page.tsx", "utf8"),
    readFile("src/components/account/account-settings-nav.tsx", "utf8"),
    readFile("src/components/account/account-settings.module.css", "utf8"),
    readFile("src/app/[locale]/settings/_pages/profile.tsx", "utf8"),
    readFile("src/app/[locale]/settings/_pages/notifications.tsx", "utf8"),
    readFile("src/app/[locale]/settings/_pages/repositories.tsx", "utf8"),
    readFile("src/app/[locale]/settings/_pages/tokens.tsx", "utf8"),
    readFile("src/app/[locale]/settings/_pages/tokens-new.tsx", "utf8"),
    readFile("src/components/layout/account-menu.tsx", "utf8"),
  ]);
  assert.match(layout, /AccountSettingsShell/);
  assert.match(layout, /showEntitlements=\{entitlements\.ok\}/);
  assert.match(index, /redirect\(accountSettingsPath\(locale, "profile"\)\)/);
  assert.match(nav, /copy\.publicProfile/);
  assert.match(nav, /copy\.developerSettings/);
  assert.match(nav, /copy\.tokensClassic/);
  assert.match(nav, /isDeveloperSection/);
  assert.match(styles, /grid-template-columns: 256px minmax\(0, 1fr\)/);
  assert.match(styles, /--nav-active-border/);
  assert.match(profile, /ProfileSettingsForm/);
  assert.match(notifications, /NotificationSettingsForm/);
  assert.match(repositories, /RepositoryInvitationSettings/);
  assert.match(tokens, /tokens\/new/);
  assert.match(newToken, /PersonalAccessTokenCreateForm/);
  const catchAll = await readFile("src/app/[locale]/settings/[...section]/page.tsx", "utf8");
  assert.match(catchAll, /"tokens\/new"/);
  assert.match(menu, /dictionary\.common\.settings/);
});

test("create pages follow GitHub's new repository, organization, and issue forms", async () => {
  const [repoPage, orgPage, issueChooser, issuePage, repoForm, orgForm, issueForm, chooser, nav, tokenForm] =
    await Promise.all([
      readFile("src/app/[locale]/repositories/new/page.tsx", "utf8"),
      readFile("src/app/[locale]/organizations/new/page.tsx", "utf8"),
      readFile("src/app/[locale]/issues/new/page.tsx", "utf8"),
      readFile("src/app/[locale]/[owner]/[repository]/issues/new/page.tsx", "utf8"),
      readFile("src/components/repositories/register-repository-form.tsx", "utf8"),
      readFile("src/components/organizations/organization-form.tsx", "utf8"),
      readFile("src/components/repositories/issue-form.tsx", "utf8"),
      readFile("src/components/repositories/repository-chooser.tsx", "utf8"),
      readFile("src/components/account/account-settings-nav.tsx", "utf8"),
      readFile("src/components/account/personal-access-token-create-form.tsx", "utf8"),
    ]);
  assert.match(repoPage, /<CreatePage/);
  assert.match(orgPage, /<CreatePage/);
  assert.match(issueChooser, /<CreatePage/);
  assert.match(issuePage, /<CreatePage wide/);
  assert.match(repoForm, /className=\{styles\.ownerRow\}/);
  assert.match(repoForm, /type="radio"/);
  assert.match(orgForm, /type="radio"/);
  assert.match(issueForm, /copy\.write/);
  assert.match(issueForm, /copy\.preview/);
  assert.match(chooser, /copy\.findARepository/);
  assert.match(nav, /copy\.backToSettings/);
  assert.match(tokenForm, /accountSettingsPath\(props\.locale, "tokens"\)/);
  assert.match(tokenForm, /dictionary\.common\.cancel/);
});
