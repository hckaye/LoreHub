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
