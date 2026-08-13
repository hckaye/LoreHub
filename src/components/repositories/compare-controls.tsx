"use client";

import { GitCompareArrows } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import type { Dictionary } from "@/i18n";
import type { CompareOption } from "@/lib/compare-revisions";

import styles from "./compare-view.module.css";

type CompareControlsProps = {
  base: string;
  comparePath: string;
  dictionary: Dictionary;
  head: string;
  options: CompareOption[];
  pullRequestHref: string;
};

export function CompareControls({
  base,
  comparePath,
  dictionary,
  head,
  options,
  pullRequestHref,
}: CompareControlsProps) {
  const router = useRouter();
  const copy = dictionary.commitHistory;
  const navigate = (source: string, target: string) => {
    router.push(`${comparePath}?source=${encodeURIComponent(source)}&target=${encodeURIComponent(target)}`);
  };
  return (
    <div className={styles.controls}>
      <GitCompareArrows aria-hidden="true" size={16} />
      <RefSelect
        id="compare-base"
        label={copy.compareBase}
        onChange={(value) => navigate(value, head)}
        options={options}
        value={base}
      />
      <RefSelect
        id="compare-head"
        label={copy.compareHead}
        onChange={(value) => navigate(base, value)}
        options={options}
        value={head}
      />
      <Link className={styles.action} href={pullRequestHref}>
        {dictionary.common.newPullRequest}
      </Link>
    </div>
  );
}

function RefSelect({
  id,
  label,
  onChange,
  options,
  value,
}: {
  id: string;
  label: string;
  onChange: (value: string) => void;
  options: CompareOption[];
  value: string;
}) {
  return (
    <div className={styles.refBox}>
      <label htmlFor={id}>{label}</label>
      <select id={id} onChange={(event) => onChange(event.target.value)} value={value}>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </div>
  );
}
