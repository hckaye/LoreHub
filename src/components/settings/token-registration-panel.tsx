"use client";

import { KeyRound } from "lucide-react";

import { CopyButton } from "@/components/ui/copy-button";

import styles from "./settings-panel.module.css";

type TokenRegistrationPanelProps = {
  body: string;
  commands: string[];
  copiedLabel: string;
  copyCommandLabel: string;
  copyTokenLabel: string;
  dismissLabel: string;
  expiryNote: string;
  hints: string[];
  onDismiss: () => void;
  title: string;
  token: string;
  tokenLabel: string;
};

export function TokenRegistrationPanel(props: TokenRegistrationPanelProps) {
  return (
    <section aria-label={props.title} className={styles.registration}>
      <div className={styles.registrationHeading}>
        <KeyRound aria-hidden="true" size={18} />
        <div>
          <h3>{props.title}</h3>
          <p>{props.body}</p>
        </div>
      </div>
      <div className={styles.secretRow}>
        <input aria-label={props.tokenLabel} readOnly value={props.token} />
        <CopyButton copiedLabel={props.copiedLabel} label={props.copyTokenLabel} value={props.token} />
        <button className={styles.secondaryButton} onClick={props.onDismiss} type="button">
          {props.dismissLabel}
        </button>
      </div>
      <p className={styles.hint}>{props.expiryNote}</p>
      <div className={styles.commands}>
        {props.commands.map((command) => (
          <div className={styles.command} key={command}>
            <code>{command}</code>
            <CopyButton copiedLabel={props.copiedLabel} label={props.copyCommandLabel} value={command} />
          </div>
        ))}
      </div>
      {props.hints.map((hint) => (
        <p className={styles.hint} key={hint}>
          {hint}
        </p>
      ))}
    </section>
  );
}
