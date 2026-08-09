"use client";

import { Languages } from "lucide-react";
import { usePathname, useSearchParams } from "next/navigation";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import { localePathFrom } from "@/lib/routes";

import styles from "./locale-switcher.module.css";

type LocaleSwitcherProps = {
  locale: Locale;
  dictionary: Dictionary;
};

export function LocaleSwitcher({ locale, dictionary }: LocaleSwitcherProps) {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const target = locale === "ja" ? "en" : "ja";
  const path = localePathFrom(pathname ?? `/${locale}`, target);
  const query = searchParams.toString();

  return (
    <a className={styles.switcher} href={`${path}${query ? `?${query}` : ""}`} hrefLang={target} lang={target}>
      <Languages aria-hidden="true" size={17} />
      {target === "ja" ? dictionary.common.localeJapanese : dictionary.common.localeEnglish}
    </a>
  );
}
