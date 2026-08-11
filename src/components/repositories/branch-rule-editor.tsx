"use client";

import { FormEvent, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { AuthSession, BranchRule } from "@/lib/api-types";
import { deleteJson, patchJson, postJson } from "@/lib/auth-client";
import { mutationFailureMessage } from "@/lib/mutation-messages";

import styles from "./branch-management.module.css";

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

type RuleInput = Pick<BranchRule, "pattern" | "requiredApprovals" | "requireCiSuccess" | "blockDirectPush">;

const emptyRule: RuleInput = {
  pattern: "",
  requiredApprovals: 1,
  requireCiSuccess: true,
  blockDirectPush: true,
};

export function BranchRuleEditor(props: RuleEditorProps) {
  const [draft, setDraft] = useState(emptyRule);
  const [saving, setSaving] = useState(false);
  const copy = props.dictionary.branchManagement;
  const session = props.session.status === "authenticated" ? props.session : null;
  const base = `/api/v1/repositories/${encodeURIComponent(props.owner)}/${encodeURIComponent(
    props.repository,
  )}/branch-rules`;

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!session || !draft.pattern.trim()) return;
    setSaving(true);
    const result = await postJson<BranchRule>(base, { ...draft, pattern: draft.pattern.trim() }, session.csrfToken);
    setSaving(false);
    if (!result.ok) {
      props.onMessage(mutationFailureMessage(result.kind, props.dictionary), true);
      return;
    }
    props.onRulesChanged([...props.initialRules, result.data].sort(sortRules));
    setDraft(emptyRule);
    props.onMessage(copy.ruleSaved, false);
  }

  async function update(rule: BranchRule, input: RuleInput) {
    if (!session) return;
    setSaving(true);
    const result = await patchJson<BranchRule>(`${base}/${encodeURIComponent(rule.id)}`, input, session.csrfToken);
    setSaving(false);
    if (!result.ok) {
      props.onMessage(mutationFailureMessage(result.kind, props.dictionary), true);
      return;
    }
    props.onRulesChanged(props.initialRules.map((item) => (item.id === rule.id ? result.data : item)).sort(sortRules));
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
          <RuleRow
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
          <RuleFields dictionary={props.dictionary} input={draft} onChange={setDraft} prefix="new-rule" />
          <button className={styles.primaryButton} disabled={saving || !draft.pattern.trim()} type="submit">
            {copy.addRule}
          </button>
        </form>
      )}
    </div>
  );
}

function RuleRow({
  rule,
  canManage,
  disabled,
  dictionary,
  onSave,
  onDelete,
}: {
  rule: BranchRule;
  canManage: boolean;
  disabled: boolean;
  dictionary: Dictionary;
  onSave(input: RuleInput): Promise<void>;
  onDelete(): Promise<void>;
}) {
  const [input, setInput] = useState<RuleInput>(rule);
  const copy = dictionary.branchManagement;
  return (
    <form
      className={styles.ruleForm}
      onSubmit={(event) => {
        event.preventDefault();
        void onSave({ ...input, pattern: input.pattern.trim() });
      }}
    >
      <RuleFields
        dictionary={dictionary}
        disabled={!canManage}
        input={input}
        onChange={setInput}
        prefix={`rule-${rule.id}`}
      />
      {canManage && (
        <div className={styles.ruleActions}>
          <button className={styles.secondaryButton} disabled={disabled || !input.pattern.trim()} type="submit">
            {copy.saveRule}
          </button>
          <button className={styles.dangerButton} disabled={disabled} onClick={() => void onDelete()} type="button">
            {copy.deleteRule}
          </button>
        </div>
      )}
    </form>
  );
}

function RuleFields({
  input,
  prefix,
  disabled = false,
  dictionary,
  onChange,
}: {
  input: RuleInput;
  prefix: string;
  disabled?: boolean;
  dictionary: Dictionary;
  onChange(input: RuleInput): void;
}) {
  const copy = dictionary.branchManagement;
  return (
    <div className={styles.ruleFields}>
      <label>
        <span>{copy.pattern}</span>
        <input
          disabled={disabled}
          maxLength={255}
          name={`${prefix}-pattern`}
          onChange={(event) => onChange({ ...input, pattern: event.target.value })}
          required
          value={input.pattern}
        />
      </label>
      <label>
        <span>{copy.approvals}</span>
        <input
          disabled={disabled}
          max={100}
          min={0}
          name={`${prefix}-approvals`}
          onChange={(event) => onChange({ ...input, requiredApprovals: Number(event.target.value) })}
          type="number"
          value={input.requiredApprovals}
        />
      </label>
      <label className={styles.checkbox}>
        <input
          checked={input.requireCiSuccess}
          disabled={disabled}
          name={`${prefix}-ci`}
          onChange={(event) => onChange({ ...input, requireCiSuccess: event.target.checked })}
          type="checkbox"
        />
        <span>{copy.requireCi}</span>
      </label>
      <label className={styles.checkbox}>
        <input
          checked={input.blockDirectPush}
          disabled={disabled}
          name={`${prefix}-push`}
          onChange={(event) => onChange({ ...input, blockDirectPush: event.target.checked })}
          type="checkbox"
        />
        <span>{copy.blockPush}</span>
      </label>
    </div>
  );
}

function sortRules(left: BranchRule, right: BranchRule) {
  return left.pattern.localeCompare(right.pattern);
}
