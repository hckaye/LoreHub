"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { UserAvatar } from "@/components/ui/user-avatar";
import type { Dictionary } from "@/i18n";
import type { Locale } from "@/i18n/config";
import type { AuthSession } from "@/lib/api-types";
import { postLogout } from "@/lib/auth-client";

import styles from "./account-menu.module.css";

type AccountMenuProps = {
  locale: Locale;
  dictionary: Dictionary;
  session: Extract<AuthSession, { status: "authenticated" }>;
};

export function AccountMenu({ locale, dictionary, session }: AccountMenuProps) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const routeKey = `${pathname ?? ""}?${searchParams.toString()}`;
  const menuRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [openRoute, setOpenRoute] = useState<string | null>(null);
  const [error, setError] = useState(false);
  const [pending, setPending] = useState(false);
  const { user } = session;
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

  async function handleLogout() {
    if (!session.csrfToken) {
      setError(true);
      return;
    }
    setError(false);
    setPending(true);
    setOpenRoute(null);
    const result = await postLogout(session.csrfToken);
    if (result.ok) {
      router.push(`/${locale}`);
      router.refresh();
      return;
    }
    setPending(false);
    setError(true);
  }

  return (
    <div className={styles.menu} ref={menuRef}>
      <button
        aria-label={user.displayName}
        aria-controls="account-menu-popover"
        aria-expanded={open}
        aria-haspopup="menu"
        className={styles.trigger}
        id="account-menu-trigger"
        onClick={() => setOpenRoute(open ? null : routeKey)}
        ref={triggerRef}
        type="button"
      >
        <UserAvatar avatarUrl={user.avatarUrl} name={user.displayName} size={27} />
        <span className={styles.name}>{user.displayName}</span>
      </button>
      {open && (
        <div aria-labelledby="account-menu-trigger" className={styles.dropdown} id="account-menu-popover" role="menu">
          <div className={styles.identity}>
            <UserAvatar avatarUrl={user.avatarUrl} name={user.displayName} size={32} />
            <div>
              <strong>{user.displayName}</strong>
              <span>@{user.username}</span>
            </div>
          </div>
          <Link href={`/${locale}/${user.username}`} onClick={() => setOpenRoute(null)} role="menuitem">
            {dictionary.common.profile}
          </Link>
          <Link href={`/${locale}/${user.username}`} onClick={() => setOpenRoute(null)} role="menuitem">
            {dictionary.common.yourRepositories}
          </Link>
          <Link href={`/${locale}/notifications`} onClick={() => setOpenRoute(null)} role="menuitem">
            {dictionary.common.notifications}
          </Link>
          <Link href={`/${locale}/organizations`} onClick={() => setOpenRoute(null)} role="menuitem">
            {dictionary.common.yourOrganizations}
          </Link>
          <Link href={`/${locale}/settings`} onClick={() => setOpenRoute(null)} role="menuitem">
            {dictionary.common.accountSettings}
          </Link>
          {error && <p className={styles.error}>{dictionary.auth.logoutFailed}</p>}
          <button disabled={pending} onClick={handleLogout} role="menuitem" type="button">
            {pending ? dictionary.common.loading : dictionary.common.signOut}
          </button>
        </div>
      )}
    </div>
  );
}
