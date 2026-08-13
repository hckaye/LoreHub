"use client";

import { ChevronDown, Plus } from "lucide-react";
import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";

import styles from "./create-menu.module.css";

type CreateMenuProps = {
  locale: Locale;
  dictionary: Dictionary;
  compact?: boolean;
};

export function CreateMenu({ locale, dictionary, compact = false }: CreateMenuProps) {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const routeKey = `${pathname ?? ""}?${searchParams.toString()}`;
  const menuRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [openRoute, setOpenRoute] = useState<string | null>(null);
  const open = openRoute === routeKey;

  useEffect(() => {
    if (!open) {
      return;
    }
    function handlePointerDown(event: PointerEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setOpenRoute(null);
      }
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        setOpenRoute(null);
        triggerRef.current?.focus();
      }
    }
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [open]);

  return (
    <div className={styles.menu} ref={menuRef}>
      <button
        aria-controls="create-menu-popover"
        aria-expanded={open}
        aria-haspopup="menu"
        aria-label={dictionary.common.create}
        className={styles.trigger}
        id="create-menu-trigger"
        onClick={() => setOpenRoute(open ? null : routeKey)}
        ref={triggerRef}
        type="button"
      >
        <Plus aria-hidden="true" size={17} />
        {!compact && <span>{dictionary.common.create}</span>}
        {compact && <ChevronDown aria-hidden="true" size={12} />}
      </button>
      {open && (
        <div aria-labelledby="create-menu-trigger" className={styles.dropdown} id="create-menu-popover" role="menu">
          <Link href={`/${locale}/issues/new`} onClick={() => setOpenRoute(null)} role="menuitem">
            {dictionary.common.newIssue}
          </Link>
          <Link href={`/${locale}/pulls/new`} onClick={() => setOpenRoute(null)} role="menuitem">
            {dictionary.common.newPullRequest}
          </Link>
          <Link href={`/${locale}/organizations/new`} onClick={() => setOpenRoute(null)} role="menuitem">
            {dictionary.common.newOrganization}
          </Link>
          <Link href={`/${locale}/repositories/new`} onClick={() => setOpenRoute(null)} role="menuitem">
            {dictionary.common.newRepository}
          </Link>
        </div>
      )}
    </div>
  );
}
