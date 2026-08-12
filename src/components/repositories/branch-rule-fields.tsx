"use client";

import type { Dictionary } from "@/i18n";

import styles from "./branch-management.module.css";
import { emptyBranchRule, type BranchRuleInput } from "./branch-rule-input";
import { RequiredStatusChecksField } from "./required-status-checks-field";

type BranchRuleFieldsProps = {
  input: BranchRuleInput;
  prefix: string;
  disabled?: boolean;
  dictionary: Dictionary;
  onChange(input: BranchRuleInput): void;
};

export function BranchRuleFields(props: BranchRuleFieldsProps) {
  const copy = props.dictionary.branchManagement;
  const disabled = props.disabled ?? false;
  return (
    <div className={styles.ruleFields}>
      <label>
        <span>{copy.pattern}</span>
        <input
          disabled={disabled}
          maxLength={255}
          name={`${props.prefix}-pattern`}
          onChange={(event) => props.onChange({ ...props.input, pattern: event.target.value })}
          required
          value={props.input.pattern}
        />
      </label>
      <label>
        <span>{copy.approvals}</span>
        <input
          disabled={disabled}
          max={100}
          min={0}
          name={`${props.prefix}-approvals`}
          onChange={(event) => props.onChange({ ...props.input, requiredApprovals: Number(event.target.value) })}
          type="number"
          value={props.input.requiredApprovals}
        />
      </label>
      <RequiredStatusChecksField
        checks={props.input.requiredStatusChecks}
        dictionary={props.dictionary}
        disabled={disabled}
        key={props.input === emptyBranchRule ? `${props.prefix}-empty` : `${props.prefix}-editing`}
        name={`${props.prefix}-status-checks`}
        onChange={(requiredStatusChecks) => props.onChange({ ...props.input, requiredStatusChecks })}
      />
      <label className={styles.checkbox}>
        <input
          checked={props.input.requireCiSuccess}
          disabled={disabled}
          name={`${props.prefix}-ci`}
          onChange={(event) => props.onChange({ ...props.input, requireCiSuccess: event.target.checked })}
          type="checkbox"
        />
        <span>{copy.requireCi}</span>
      </label>
      <label className={styles.checkbox}>
        <input
          checked={props.input.blockDirectPush}
          disabled={disabled}
          name={`${props.prefix}-push`}
          onChange={(event) => props.onChange({ ...props.input, blockDirectPush: event.target.checked })}
          type="checkbox"
        />
        <span>{copy.blockPush}</span>
      </label>
    </div>
  );
}
