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
  assert.match(issuePage, /<IssueForm/);
  assert.doesNotMatch(issuePage, /CreatePage/);
  assert.match(repoForm, /className=\{styles\.ownerRow\}/);
  assert.match(repoForm, /type="radio"/);
  assert.match(orgForm, /type="radio"/);
  assert.match(issueForm, /UserAvatar/);
  assert.match(issueForm, /variant="commentBox"/);
  assert.match(issueForm, /copy\.noneYet/);
  assert.match(issueForm, /styles\.sidebar/);
  assert.match(chooser, /copy\.findARepository/);
  assert.match(nav, /copy\.backToSettings/);
  assert.match(tokenForm, /accountSettingsPath\(props\.locale, "tokens"\)/);
  assert.match(tokenForm, /dictionary\.common\.cancel/);
});

test("new issue page matches GitHub's avatar, comment box, and sidebar layout", async () => {
  const [formStyles, field, fieldStyles] = await Promise.all([
    readFile("src/components/repositories/issue-form.module.css", "utf8"),
    readFile("src/components/repositories/issue-markdown-field.tsx", "utf8"),
    readFile("src/components/repositories/issue-markdown-field.module.css", "utf8"),
  ]);
  assert.match(formStyles, /max-width: 1280px/);
  assert.match(formStyles, /padding: 24px 16px/);
  assert.match(formStyles, /minmax\(0, 3fr\) 256px/);
  assert.match(formStyles, /height: 32px/);
  assert.match(formStyles, /justify-content: flex-end/);
  assert.match(formStyles, /--success-border/);
  assert.match(formStyles, /font-size: 12px/);
  assert.match(formStyles, /font-weight: 600/);
  assert.match(fieldStyles, /min-height: 200px/);
  assert.match(fieldStyles, /background: var\(--canvas-subtle\)/);
  assert.match(fieldStyles, /border-bottom-color: transparent/);
  assert.match(fieldStyles, /border-right-color: var\(--border\)/);
  assert.match(field, /markdownSupported/);
  assert.match(field, /variant === "commentBox"/);
});

test("user profile uses GitHub vcard, underline tabs, and divided repository rows", async () => {
  const [page, styles, rows, rowStyles, navStyles] = await Promise.all([
    readFile("src/components/profile/user-profile-page.tsx", "utf8"),
    readFile("src/components/profile/user-profile-page.module.css", "utf8"),
    readFile("src/components/repositories/repository-rows.tsx", "utf8"),
    readFile("src/components/repositories/repository-rows.module.css", "utf8"),
    readFile("src/components/ui/underline-nav.module.css", "utf8"),
  ]);
  assert.match(page, /<UnderlineNav/);
  assert.match(page, /<RepositoryRows/);
  assert.match(page, /size=\{296\}/);
  assert.match(styles, /grid-template-columns: 296px minmax\(0, 1fr\)/);
  assert.match(styles, /max-width: 1280px/);
  assert.match(styles, /padding: 24px 16px/);
  assert.match(styles, /font-size: 24px/);
  assert.match(styles, /font-weight: 300/);
  assert.match(rows, /type="search"/);
  assert.match(rowStyles, /height: 32px/);
  assert.match(rowStyles, /padding: 24px 0/);
  assert.match(rowStyles, /border-radius: 2em/);
  assert.match(navStyles, /--nav-active-border/);
  assert.match(navStyles, /--neutral-muted/);
});

test("organization profile uses header tabs and dedicated settings and teams routes", async () => {
  const [page, profile, teamsPage, teamsRoute, settings] = await Promise.all([
    readFile("src/components/organizations/organization-page.tsx", "utf8"),
    readFile("src/components/organizations/organization-profile.tsx", "utf8"),
    readFile("src/components/organizations/organization-teams-page.tsx", "utf8"),
    readFile("src/app/[locale]/organizations/[organization]/teams/page.tsx", "utf8"),
    readFile("src/app/[locale]/organizations/[organization]/settings/page.tsx", "utf8"),
  ]);
  assert.match(page, /<RepositoryRows/);
  assert.doesNotMatch(page, /TeamCreateForm/);
  assert.doesNotMatch(page, /OrganizationSettingsForm/);
  assert.match(profile, /tab === "repositories"/);
  assert.match(profile, /tab === "teams"/);
  assert.match(profile, /settingsButton/);
  assert.match(teamsPage, /<TeamCreateForm/);
  assert.match(teamsRoute, /OrganizationTeamsPage/);
  assert.match(settings, /OrganizationSettingsForm/);
});
