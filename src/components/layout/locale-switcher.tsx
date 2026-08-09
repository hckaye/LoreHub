"use client";

import { Languages } from "lucide-react";
import { usePathname } from "next/navigation";

import type { Locale } from "@/i18n/config";

import styles from "./locale-switcher.module.css";

type LocaleSwitcherProps = {
  locale: Locale;
};

export function LocaleSwitcher({ locale }: LocaleSwitcherProps) {
  const pathname = usePathname();
  const target = locale === "ja" ? "en" : "ja";
  const segments = pathname.split("/");
  segments[1] = target;

  return (
    <a className={styles.switcher} href={segments.join("/")} hrefLang={target} lang={target}>
      <Languages aria-hidden="true" size={17} />
      {target === "ja" ? "日本語" : "English"}
    </a>
  );
}
