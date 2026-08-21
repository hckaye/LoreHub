export const locales = ["en", "ja"] as const;

export type Locale = (typeof locales)[number];

export const defaultLocale: Locale = "en";

export function isLocale(value: string): value is Locale {
  return locales.includes(value as Locale);
}

export function localeFromAcceptLanguage(value: string | null): Locale {
  if (!value) {
    return defaultLocale;
  }
  const preferences = value
    .split(",")
    .map((entry, index) => parseLanguagePreference(entry, index))
    .filter((entry) => entry.quality > 0)
    .sort((left, right) => right.quality - left.quality || left.index - right.index);
  for (const preference of preferences) {
    const locale = preference.tag.split("-")[0];
    if (isLocale(locale)) {
      return locale;
    }
  }
  return defaultLocale;
}

function parseLanguagePreference(entry: string, index: number): { tag: string; quality: number; index: number } {
  const [rawTag, ...parameters] = entry.trim().toLowerCase().split(";");
  let quality = 1;
  for (const parameter of parameters) {
    const [name, rawValue] = parameter.trim().split("=", 2);
    if (name !== "q") continue;
    const parsed = Number(rawValue);
    quality = Number.isFinite(parsed) && parsed >= 0 && parsed <= 1 ? parsed : 0;
  }
  return { tag: rawTag.trim(), quality, index };
}
