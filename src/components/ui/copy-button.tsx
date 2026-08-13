"use client";

import { Check, Copy } from "lucide-react";
import { useState } from "react";

import styles from "./copy-button.module.css";

type CopyButtonProps = {
  copiedLabel: string;
  label: string;
  value: string;
};

export function CopyButton({ copiedLabel, label, value }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      return;
    }
    setCopied(true);
    window.setTimeout(() => setCopied(false), 2_000);
  };
  return (
    <button
      aria-label={copied ? copiedLabel : label}
      className={styles.button}
      data-copied={copied}
      onClick={() => void copy()}
      title={copied ? copiedLabel : label}
      type="button"
    >
      {copied ? <Check aria-hidden="true" size={14} /> : <Copy aria-hidden="true" size={14} />}
    </button>
  );
}
