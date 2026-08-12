import type { BranchRule } from "./api-types";

export type StatusContextError = "too_many" | "too_long" | "invalid";

export function parseRequiredStatusChecks(
  value: string,
): { ok: true; checks: string[] } | { ok: false; error: StatusContextError } {
  const checks: string[] = [];
  const seen = new Set<string>();
  for (const raw of value.split(/\r?\n/u)) {
    const context = raw.trim();
    if (!context) continue;
    if ([...context].length > 100) return { ok: false, error: "too_long" };
    if (/\p{Cc}/u.test(context)) return { ok: false, error: "invalid" };
    const key = context.toLocaleLowerCase("en-US");
    if (seen.has(key)) continue;
    seen.add(key);
    checks.push(context);
    if (checks.length > 50) return { ok: false, error: "too_many" };
  }
  return { ok: true, checks };
}

export function parseBranchRule(value: unknown): BranchRule | null {
  if (!isRecord(value) || !hasExactKeys(value, branchRuleKeys)) return null;
  if (!validBranchRuleFields(value) || !Array.isArray(value.requiredStatusChecks)) return null;
  if (!value.requiredStatusChecks.every((context) => typeof context === "string")) return null;
  const checks = value.requiredStatusChecks as string[];
  const parsed = parseRequiredStatusChecks(checks.join("\n"));
  if (!parsed.ok || parsed.checks.length !== checks.length) return null;
  if (!checks.every((context, index) => context === parsed.checks[index])) return null;
  return value as BranchRule;
}

export function parseBranchRuleList(value: unknown): BranchRule[] | null {
  if (!isRecord(value) || !hasExactKeys(value, ["items"]) || !Array.isArray(value.items)) return null;
  const rules = value.items.map(parseBranchRule);
  return rules.some((rule) => rule === null) ? null : (rules as BranchRule[]);
}

function validBranchRuleFields(value: Record<string, unknown>): boolean {
  return (
    typeof value.id === "string" &&
    value.id.length > 0 &&
    typeof value.repositoryId === "string" &&
    value.repositoryId.length > 0 &&
    typeof value.pattern === "string" &&
    value.pattern.length > 0 &&
    Number.isSafeInteger(value.requiredApprovals) &&
    Number(value.requiredApprovals) >= 0 &&
    Number(value.requiredApprovals) <= 100 &&
    typeof value.requireCiSuccess === "boolean" &&
    typeof value.blockDirectPush === "boolean" &&
    isTimestamp(value.createdAt) &&
    isTimestamp(value.updatedAt)
  );
}

function isTimestamp(value: unknown): value is string {
  return typeof value === "string" && value.length > 0 && Number.isFinite(Date.parse(value));
}

function hasExactKeys(value: Record<string, unknown>, expected: readonly string[]): boolean {
  const keys = Object.keys(value);
  return keys.length === expected.length && expected.every((key) => Object.hasOwn(value, key));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

const branchRuleKeys = [
  "id",
  "repositoryId",
  "pattern",
  "requiredApprovals",
  "requireCiSuccess",
  "requiredStatusChecks",
  "blockDirectPush",
  "createdAt",
  "updatedAt",
] as const;
