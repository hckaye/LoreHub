"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";

import { MarkdownContent } from "../wiki/markdown-content";
import styles from "./issue-detail.module.css";

type MarkdownFieldProps = {
  dictionary: Dictionary;
  id: string;
  label: string;
  labelHidden?: boolean;
  onChange: (value: string) => void;
  placeholder?: string;
  value: string;
};

export function MarkdownField(props: MarkdownFieldProps) {
  const copy = props.dictionary.issueDetail;
  const [mode, setMode] = useState<"write" | "preview">("write");
  const panelID = `${props.id}-panel`;
  const writeTabID = `${props.id}-write-tab`;
  const previewTabID = `${props.id}-preview-tab`;
  return (
    <div className={styles.markdownField}>
      <label className={props.labelHidden ? styles.visuallyHidden : styles.fieldLabel} htmlFor={props.id}>
        {props.label}
      </label>
      <div aria-label={props.label} className={styles.tabs} role="tablist">
        <button
          aria-controls={panelID}
          aria-selected={mode === "write"}
          className={styles.tab}
          id={writeTabID}
          onClick={() => setMode("write")}
          role="tab"
          type="button"
        >
          {copy.writeTab}
        </button>
        <button
          aria-controls={panelID}
          aria-selected={mode === "preview"}
          className={styles.tab}
          id={previewTabID}
          onClick={() => setMode("preview")}
          role="tab"
          type="button"
        >
          {copy.previewTab}
        </button>
      </div>
      <div
        aria-labelledby={mode === "write" ? writeTabID : previewTabID}
        className={styles.panel}
        id={panelID}
        role="tabpanel"
      >
        {mode === "write" ? (
          <textarea
            className={styles.textarea}
            id={props.id}
            maxLength={1_000_000}
            onChange={(event) => props.onChange(event.target.value)}
            placeholder={props.placeholder}
            value={props.value}
          />
        ) : props.value.trim() ? (
          <div className={styles.preview}>
            <MarkdownContent body={props.value} />
          </div>
        ) : (
          <p className={styles.previewEmpty}>{copy.previewEmpty}</p>
        )}
      </div>
    </div>
  );
}
