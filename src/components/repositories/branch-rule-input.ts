import type { BranchRule } from "@/lib/api-types";
import { parseRequiredStatusChecks } from "@/lib/branch-rule-contract";

export { parseRequiredStatusChecks } from "@/lib/branch-rule-contract";
export type { StatusContextError } from "@/lib/branch-rule-contract";

export type BranchRuleInput = Pick<
  BranchRule,
  "pattern" | "requiredApprovals" | "requireCiSuccess" | "requiredStatusChecks" | "blockDirectPush"
>;

export const emptyBranchRule: BranchRuleInput = {
  pattern: "",
  requiredApprovals: 1,
  requireCiSuccess: true,
  requiredStatusChecks: [],
  blockDirectPush: true,
};

export function normalizeBranchRuleInput(input: BranchRuleInput): BranchRuleInput | null {
  const parsed = parseRequiredStatusChecks(input.requiredStatusChecks.join("\n"));
  if (!parsed.ok) return null;
  return { ...input, pattern: input.pattern.trim(), requiredStatusChecks: parsed.checks };
}
