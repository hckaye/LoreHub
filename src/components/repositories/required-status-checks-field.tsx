"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";

import styles from "./branch-management.module.css";
import { parseRequiredStatusChecks, type StatusContextError } from "./branch-rule-input";

type RequiredStatusChecksFieldProps = {
  checks: string[];
  dictionary: Dictionary;
  disabled: boolean;
  name: string;
  onChange(checks: string[]): void;
};

export function RequiredStatusChecksField(props: RequiredStatusChecksFieldProps) {
  const [value, setValue] = useState(props.checks.join("\n"));
  const copy = props.dictionary.commitStatuses;
  const helpID = `${props.name}-help`;

  return (
    <label className={styles.statusChecksField}>
      <span>{copy.requiredContexts}</span>
      <textarea
        aria-describedby={helpID}
        disabled={props.disabled}
        name={props.name}
        onBlur={() => {
          const parsed = parseRequiredStatusChecks(value);
          if (parsed.ok) setValue(parsed.checks.join("\n"));
        }}
        onChange={(event) => {
          const next = event.target.value;
          const parsed = parseRequiredStatusChecks(next);
          setValue(next);
          event.target.setCustomValidity(parsed.ok ? "" : validationMessage(parsed.error, props.dictionary));
          if (parsed.ok) props.onChange(parsed.checks);
        }}
        placeholder={copy.requiredContextsPlaceholder}
        rows={Math.min(Math.max(value.split("\n").length, 2), 6)}
        value={value}
      />
      <small id={helpID}>{copy.requiredContextsHelp}</small>
    </label>
  );
}

function validationMessage(error: StatusContextError, dictionary: Dictionary): string {
  if (error === "too_many") return dictionary.commitStatuses.tooManyContexts;
  if (error === "too_long") return dictionary.commitStatuses.contextTooLong;
  return dictionary.commitStatuses.invalidContext;
}
