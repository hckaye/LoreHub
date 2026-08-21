import assert from "node:assert/strict";
import test from "node:test";

import { defaultLocale, isLocale, localeFromAcceptLanguage, locales } from "../src/i18n/config";
import en from "../src/i18n/dictionaries/en";
import ja from "../src/i18n/dictionaries/ja";

test("all configured locales are accepted", () => {
  for (const locale of locales) {
    assert.equal(isLocale(locale), true);
  }
});

test("unknown locale is rejected", () => {
  assert.equal(isLocale("fr"), false);
  assert.equal(defaultLocale, "en");
});

test("Accept-Language quality values select the preferred supported locale", () => {
  assert.equal(localeFromAcceptLanguage("en-US,en;q=0.9,ja;q=0.1"), "en");
  assert.equal(localeFromAcceptLanguage("en;q=0.4,ja-JP;q=0.9"), "ja");
  assert.equal(localeFromAcceptLanguage("fr-FR,ja;q=0.8,en;q=0.7"), "ja");
  assert.equal(localeFromAcceptLanguage("fr-FR,*;q=0.5"), defaultLocale);
});

test("invalid or disabled language preferences do not override the default", () => {
  assert.equal(localeFromAcceptLanguage(null), defaultLocale);
  assert.equal(localeFromAcceptLanguage("ja;q=0,en;q=0"), defaultLocale);
  assert.equal(localeFromAcceptLanguage("ja;q=invalid,en;q=0.5"), "en");
});

test("English and Japanese dictionaries have the same complete key set", () => {
  const englishKeys = flattenKeys(en);
  const japaneseKeys = flattenKeys(ja);
  assert.deepEqual(japaneseKeys, englishKeys);
  for (const key of japaneseKeys) {
    assert.notEqual(getValue(ja, key), "", `${key} must not be empty`);
  }
});

test("Japanese dictionary does not silently reuse English copy", () => {
  const exceptions = new Set([
    "common.actions",
    "common.issues",
    "common.localeJapanese",
    "common.productName",
    "common.wiki",
    "entitlementSettings.subjectPlaceholder",
    "eventTopics.entities.issue",
    "eventTopics.entities.webhook",
    "fileLocks.pathPlaceholder",
    "actionsPage.title",
    "forms.loreUrl",
    "forms.loreUrlPlaceholder",
    "projectsPage.board.issue",
    "repository.actionsTitle",
    "accountSettings.url",
    "settingsPage.loreUrl",
    "webhookSettings.eventLabels.actions",
    "webhookSettings.payloadUrlPlaceholder",
    "webhookSettings.eventLabels.wiki",
    "wikiPage.title",
  ]);
  for (const key of flattenKeys(en)) {
    if (!exceptions.has(key)) {
      assert.notEqual(getValue(ja, key), getValue(en, key), `${key} is not translated`);
    }
  }
});

function flattenKeys(value: Record<string, unknown>, prefix = ""): string[] {
  return Object.entries(value).flatMap(([key, entry]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return isRecord(entry) ? flattenKeys(entry, path) : [path];
  });
}

function getValue(value: Record<string, unknown>, path: string): unknown {
  return path.split(".").reduce<unknown>((current, key) => (isRecord(current) ? current[key] : undefined), value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
