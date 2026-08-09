import assert from "node:assert/strict";
import test from "node:test";

import { defaultLocale, isLocale, locales } from "../src/i18n/config";

test("all configured locales are accepted", () => {
  for (const locale of locales) {
    assert.equal(isLocale(locale), true);
  }
});

test("unknown locale is rejected", () => {
  assert.equal(isLocale("fr"), false);
  assert.equal(defaultLocale, "en");
});
