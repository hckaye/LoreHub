import type { Locale } from "@/i18n/config";

const shortRevisionLength = 7;

const RELATIVE_UNITS: Array<{ limit: number; divisor: number; unit: Intl.RelativeTimeFormatUnit }> = [
  { limit: 60, divisor: 1, unit: "second" },
  { limit: 3600, divisor: 60, unit: "minute" },
  { limit: 86400, divisor: 3600, unit: "hour" },
  { limit: 2592000, divisor: 86400, unit: "day" },
  { limit: 31536000, divisor: 2592000, unit: "month" },
  { limit: Number.POSITIVE_INFINITY, divisor: 31536000, unit: "year" },
];

export function formatRelativeTime(value: string | Date, locale: Locale, now = new Date()): string {
  const date = typeof value === "string" ? new Date(value) : value;
  if (Number.isNaN(date.getTime())) {
    return typeof value === "string" ? value : "";
  }
  const elapsedSeconds = Math.round((date.getTime() - now.getTime()) / 1000);
  const magnitude = Math.abs(elapsedSeconds);
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  for (const { limit, divisor, unit } of RELATIVE_UNITS) {
    if (magnitude < limit) {
      return formatter.format(Math.trunc(elapsedSeconds / divisor), unit);
    }
  }
  return formatter.format(Math.trunc(elapsedSeconds / 31536000), "year");
}

export function formatDate(value: string | Date, locale: Locale): string {
  const date = typeof value === "string" ? new Date(value) : value;
  if (Number.isNaN(date.getTime())) {
    return typeof value === "string" ? value : "";
  }
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(date);
}

export function formatDateTime(value: string | Date, locale: Locale): string {
  const date = typeof value === "string" ? new Date(value) : value;
  if (Number.isNaN(date.getTime())) {
    return typeof value === "string" ? value : "";
  }
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(date);
}

export function formatTimestamp(value: string | Date, locale: Locale): string {
  const date = typeof value === "string" ? new Date(value) : value;
  if (Number.isNaN(date.getTime())) {
    return typeof value === "string" ? value : "";
  }
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" }).format(date);
}

export function abbreviateCount(count: number, locale: Locale): string {
  return new Intl.NumberFormat(locale, { notation: "compact", maximumFractionDigits: 1 }).format(count);
}

export function shortRevision(revision: string): string {
  return revision.slice(0, shortRevisionLength);
}

export function normalizeLabelColor(color: string): string {
  const value = color.trim().replace(/^#/, "");
  return /^[0-9a-fA-F]{3}$|^[0-9a-fA-F]{6}$/.test(value) ? `#${value}` : "#d0d7de";
}

export function labelTextColor(color: string): string {
  const value = normalizeLabelColor(color).slice(1);
  const expanded = value.length === 3 ? value.replace(/./g, (c) => c + c) : value;
  const r = parseInt(expanded.slice(0, 2), 16);
  const g = parseInt(expanded.slice(2, 4), 16);
  const b = parseInt(expanded.slice(4, 6), 16);
  const luminance = (0.299 * r + 0.587 * g + 0.114 * b) / 255;
  return luminance > 0.6 ? "#1f2328" : "#ffffff";
}
