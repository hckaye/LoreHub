"use client";

import { Check, Copy, FileCode2 } from "lucide-react";
import { useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { TreeEntry } from "@/lib/api-types";
import { createRepositoryReadmeURLTransform } from "@/lib/repository-readme";

import { MarkdownContent } from "../wiki/markdown-content";
import styles from "./blob-view.module.css";

type BlobViewFileProps = {
  dictionary: Dictionary;
  locale: Locale;
  owner: string;
  repository: string;
  fileName: string;
  size: number;
  lineCount: number;
  content: string | undefined;
  binary: boolean;
  truncated: boolean;
  rawPath: string;
  isMarkdown: boolean;
  revision: string;
  readmePath: string;
  entries: TreeEntry[];
};

export function BlobViewFile(props: BlobViewFileProps) {
  const copy = props.dictionary.codeBrowser;
  const [mode, setMode] = useState<"preview" | "code">("preview");
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");
  const showCode = !props.isMarkdown || mode === "code";
  const canCopy = !props.binary && !props.truncated && Boolean(props.content);

  async function handleCopy() {
    if (!props.content) {
      return;
    }
    try {
      await navigator.clipboard.writeText(props.content);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
    window.setTimeout(() => setCopyState("idle"), 2000);
  }

  return (
    <div className={styles.fileCard}>
      <div className={styles.fileHeader}>
        <div className={styles.fileInfo}>
          <h2 className={styles.fileName}>{props.fileName}</h2>
          <p className={styles.fileMeta}>
            {props.lineCount} {copy.lines} · {props.size.toLocaleString()} {copy.bytes}
          </p>
        </div>
        <div className={styles.fileActions}>
          {props.isMarkdown && (
            <div className={styles.toggleGroup} role="group" aria-label={copy.blobTitle}>
              <button
                aria-pressed={mode === "preview"}
                className={styles.toggleButton}
                onClick={() => setMode("preview")}
                type="button"
              >
                {copy.preview}
              </button>
              <button
                aria-pressed={mode === "code"}
                className={styles.toggleButton}
                onClick={() => setMode("code")}
                type="button"
              >
                {copy.code}
              </button>
            </div>
          )}
          {canCopy && (
            <button
              className={styles.actionButton}
              data-state={copyState}
              onClick={handleCopy}
              type="button"
            >
              {copyState === "copied" ? (
                <Check aria-hidden="true" size={14} />
              ) : (
                <Copy aria-hidden="true" size={14} />
              )}
              {copyState === "copied" ? copy.copied : copy.copyContents}
            </button>
          )}
          <a className={styles.actionLink} href={props.rawPath}>
            {copy.raw}
          </a>
        </div>
      </div>
      <div className={styles.fileBody}>
        {props.truncated ? (
          <p className={styles.status} data-tone="warning">
            {copy.tooLarge}
          </p>
        ) : props.binary ? (
          <p className={styles.status}>
            <FileCode2 aria-hidden="true" size={20} />
            {copy.binary}
          </p>
        ) : showCode ? (
          <CodeDisplay
            ariaLabel={copy.lineNumbers}
            content={props.content ?? ""}
          />
        ) : props.content ? (
          <div className={styles.markdownBody}>
            <MarkdownContent
              body={props.content}
              urlTransform={createRepositoryReadmeURLTransform({
                locale: props.locale,
                owner: props.owner,
                repository: props.repository,
                revision: props.revision,
                readmePath: props.readmePath,
                entries: props.entries,
              })}
            />
          </div>
        ) : (
          <p className={styles.status}>{copy.emptyReadme}</p>
        )}
      </div>
    </div>
  );
}

type CodeDisplayProps = {
  ariaLabel: string;
  content: string;
};

function CodeDisplay({ ariaLabel, content }: CodeDisplayProps) {
  const lines = content.split("\n");
  return (
    <div className={styles.codeScroll}>
      <div className={styles.codeRow}>
        <div aria-label={ariaLabel} className={styles.gutter} role="group">
          {lines.map((_, index) => (
            <div className={styles.gutterLine} key={index}>
              {index + 1}
            </div>
          ))}
        </div>
        <pre className={styles.codePre}>{content}</pre>
      </div>
    </div>
  );
}
