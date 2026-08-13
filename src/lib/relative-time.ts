import type { Locale } from "@/i18n/config";

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 31_536_000_000],
  ["month", 2_592_000_000],
  ["week", 604_800_000],
  ["day", 86_400_000],
  ["hour", 3_600_000],
  ["minute", 60_000],
];

export function formatRelativeTime(value: string, locale: Locale, now = Date.now()): string {
  const time = Date.parse(value);
  if (!Number.isFinite(time)) {
    return "";
  }
  const delta = time - now;
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  for (const [unit, size] of units) {
    if (Math.abs(delta) >= size) {
      return formatter.format(Math.round(delta / size), unit);
    }
  }
  return formatter.format(Math.round(delta / 1_000), "second");
}

export function formatTimestamp(value: string, locale: Locale): string {
  const time = Date.parse(value);
  if (!Number.isFinite(time)) {
    return "";
  }
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" }).format(time);
}
