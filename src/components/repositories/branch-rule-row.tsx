"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { BranchRule } from "@/lib/api-types";

import styles from "./branch-management.module.css";
import { BranchRuleFields } from "./branch-rule-fields";
import type { BranchRuleInput } from "./branch-rule-input";
import { normalizeBranchRuleInput } from "./branch-rule-input";

type BranchRuleRowProps = {
  rule: BranchRule;
  canManage: boolean;
  disabled: boolean;
  dictionary: Dictionary;
  onSave(input: BranchRuleInput): Promise<void>;
  onDelete(): Promise<void>;
};

export function BranchRuleRow(props: BranchRuleRowProps) {
  const [input, setInput] = useState<BranchRuleInput>(props.rule);
  const copy = props.dictionary.branchManagement;
  return (
    <form
      className={styles.ruleForm}
      onSubmit={(event) => {
        event.preventDefault();
        const normalized = normalizeBranchRuleInput(input);
        if (normalized) void props.onSave(normalized);
      }}
    >
      <BranchRuleFields
        dictionary={props.dictionary}
        disabled={!props.canManage}
        input={input}
        onChange={setInput}
        prefix={`rule-${props.rule.id}`}
      />
      {props.canManage && (
        <div className={styles.ruleActions}>
          <button className={styles.secondaryButton} disabled={props.disabled || !input.pattern.trim()} type="submit">
            {copy.saveRule}
          </button>
          <button
            className={styles.dangerButton}
            disabled={props.disabled}
            onClick={() => void props.onDelete()}
            type="button"
          >
            {copy.deleteRule}
          </button>
        </div>
      )}
    </form>
  );
}
