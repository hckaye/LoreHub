"use client";

import { useState } from "react";

import type { Dictionary } from "@/i18n";

import { MarkdownContent } from "../wiki/markdown-content";
import conversationStyles from "./issue-detail.module.css";
import commentBoxStyles from "./issue-markdown-field.module.css";

type MarkdownFieldProps = {
  dictionary: Dictionary;
  id: string;
  label: string;
  labelHidden?: boolean;
  onChange: (value: string) => void;
  placeholder?: string;
  value: string;
  variant?: "default" | "commentBox";
};

export function MarkdownField(props: MarkdownFieldProps) {
  const copy = props.dictionary.issueDetail;
  const [mode, setMode] = useState<"write" | "preview">("write");
  const panelID = `${props.id}-panel`;
  const writeTabID = `${props.id}-write-tab`;
  const previewTabID = `${props.id}-preview-tab`;
  const boxed = props.variant === "commentBox";
  const styles = boxed ? commentBoxStyles : conversationStyles;
  const tablistClass = boxed ? commentBoxStyles.header : conversationStyles.tabs;
  return (
    <div className={boxed ? commentBoxStyles.commentBox : conversationStyles.markdownField}>
      <label className={props.labelHidden ? styles.visuallyHidden : conversationStyles.fieldLabel} htmlFor={props.id}>
        {props.label}
      </label>
      <div aria-label={props.label} className={tablistClass} role="tablist">
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
      {boxed && mode === "write" && (
        <div className={commentBoxStyles.footer}>
          <span className={commentBoxStyles.hint}>
            <MarkdownMark />
            {copy.markdownSupported}
          </span>
        </div>
      )}
    </div>
  );
}

const markdownIconPath = [
  "M14.85 3c.63 0 1.15.52 1.14 1.15v7.7c0 .63-.51 1.15-1.15 1.15H1.15C.52 13",
  " 0 12.48 0 11.84V4.15C0 3.52.52 3 1.15 3ZM9 11V5H7L5.5 7 4 5H2v6h2V8l1.5 1.92",
  "L7 8v3Zm2.99.5L14.5 8H13V5h-2v3H9.5Z",
].join("");

function MarkdownMark() {
  return (
    <svg aria-hidden="true" fill="currentColor" height="16" viewBox="0 0 16 16" width="16">
      <path d={markdownIconPath} />
    </svg>
  );
}
