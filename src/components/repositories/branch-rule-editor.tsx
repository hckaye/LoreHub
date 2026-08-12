"use client";

import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, BranchRule } from "@/lib/api-types";
import { deleteJson, patchJson, postJson } from "@/lib/auth-client";
import { parseBranchRule } from "@/lib/branch-rule-contract";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import styles from "./branch-management.module.css";
import { BranchRuleFields } from "./branch-rule-fields";
import { emptyBranchRule, normalizeBranchRuleInput, type BranchRuleInput } from "./branch-rule-input";
import { BranchRuleRow } from "./branch-rule-row";

type RuleEditorProps = {
  owner: string;
  repository: string;
  initialRules: BranchRule[];
  canManage: boolean;
  session: AuthSession;
  dictionary: Dictionary;
  onRulesChanged(rules: BranchRule[]): void;
  onMessage(message: string, error: boolean): void;
};

export function BranchRuleEditor(props: RuleEditorProps) {
  const [draft, setDraft] = useState(emptyBranchRule);
  const [saving, setSaving] = useState(false);
  const copy = props.dictionary.branchManagement;
  const session = props.session.status === "authenticated" ? props.session : null;
  const base = `/api/v1/repositories/${encodeURIComponent(props.owner)}/${encodeURIComponent(
    props.repository,
  )}/branch-rules`;

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const input = normalizeBranchRuleInput(draft);
    if (!session || !input?.pattern) return;
    setSaving(true);
    const result = await postJson<BranchRule>(base, input, session.csrfToken);
    setSaving(false);
    if (!result.ok) {
      props.onMessage(mutationFailureMessage(result.kind, props.dictionary), true);
      return;
    }
    const rule = parseBranchRule(result.data);
    if (!rule) {
      props.onMessage(props.dictionary.commitStatuses.invalidRuleResponse, true);
      return;
    }
    props.onRulesChanged([...props.initialRules, rule].sort(sortRules));
    setDraft(emptyBranchRule);
    props.onMessage(copy.ruleSaved, false);
  }

  async function update(rule: BranchRule, input: BranchRuleInput) {
    if (!session) return;
    setSaving(true);
    const result = await patchJson<BranchRule>(`${base}/${encodeURIComponent(rule.id)}`, input, session.csrfToken);
    setSaving(false);
    if (!result.ok) {
      props.onMessage(mutationFailureMessage(result.kind, props.dictionary), true);
      return;
    }
    const updated = parseBranchRule(result.data);
    if (!updated) {
      props.onMessage(props.dictionary.commitStatuses.invalidRuleResponse, true);
      return;
    }
    props.onRulesChanged(props.initialRules.map((item) => (item.id === rule.id ? updated : item)).sort(sortRules));
    props.onMessage(copy.ruleSaved, false);
  }

  async function remove(rule: BranchRule) {
    if (!session || !window.confirm(copy.deleteRuleConfirm.replace("{pattern}", rule.pattern))) return;
    setSaving(true);
    const result = await deleteJson<null>(`${base}/${encodeURIComponent(rule.id)}`, session.csrfToken);
    setSaving(false);
    if (!result.ok) {
      props.onMessage(mutationFailureMessage(result.kind, props.dictionary), true);
      return;
    }
    props.onRulesChanged(props.initialRules.filter((item) => item.id !== rule.id));
    props.onMessage(copy.ruleDeleted, false);
  }

  return (
    <div className={styles.rules}>
      {props.initialRules.length === 0 ? (
        <p className={styles.muted}>{copy.noRules}</p>
      ) : (
        props.initialRules.map((rule) => (
          <BranchRuleRow
            canManage={props.canManage && Boolean(session)}
            dictionary={props.dictionary}
            disabled={saving}
            key={rule.id}
            onDelete={() => remove(rule)}
            onSave={(input) => update(rule, input)}
            rule={rule}
          />
        ))
      )}
      {props.canManage && session && (
        <form className={styles.ruleForm} onSubmit={create}>
          <BranchRuleFields dictionary={props.dictionary} input={draft} onChange={setDraft} prefix="new-rule" />
          <button className={styles.primaryButton} disabled={saving || !draft.pattern.trim()} type="submit">
            {copy.addRule}
          </button>
        </form>
      )}
    </div>
  );
}

function sortRules(left: BranchRule, right: BranchRule) {
  return left.pattern.localeCompare(right.pattern);
}
