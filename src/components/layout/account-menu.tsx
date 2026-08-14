"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

import { PopupMenu } from "@/components/ui/popup-menu";
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
  const [error, setError] = useState(false);
  const [pending, setPending] = useState(false);
  const { user } = session;

  async function handleLogout(close: () => void) {
    if (!session.csrfToken) {
      setError(true);
      return;
    }
    setError(false);
    setPending(true);
    const result = await postLogout(session.csrfToken);
    if (result.ok) {
      close();
      router.push(`/${locale}`);
      router.refresh();
      return;
    }
    setPending(false);
    setError(true);
  }

  return (
    <PopupMenu
      className={styles.menu}
      panelClassName={styles.dropdown}
      trigger={
        <>
          <UserAvatar avatarUrl={user.avatarUrl} name={user.displayName} size={27} />
          <span className={styles.name}>{user.displayName}</span>
        </>
      }
      triggerClassName={styles.trigger}
      triggerProps={{ "aria-label": user.displayName }}
    >
      {(close) => (
        <>
          <div className={styles.identity}>
            <UserAvatar avatarUrl={user.avatarUrl} name={user.displayName} size={32} />
            <div>
              <strong>{user.displayName}</strong>
              <span>@{user.username}</span>
            </div>
          </div>
          <Link href={`/${locale}/${user.username}`} onClick={close} role="menuitem">
            {dictionary.common.profile}
          </Link>
          <Link href={`/${locale}/${user.username}`} onClick={close} role="menuitem">
            {dictionary.common.yourRepositories}
          </Link>
          <Link href={`/${locale}/notifications`} onClick={close} role="menuitem">
            {dictionary.common.notifications}
          </Link>
          <Link href={`/${locale}/organizations`} onClick={close} role="menuitem">
            {dictionary.common.yourOrganizations}
          </Link>
          <Link href={`/${locale}/settings`} onClick={close} role="menuitem">
            {dictionary.common.settings}
          </Link>
          {error && <p className={styles.error}>{dictionary.auth.logoutFailed}</p>}
          <button disabled={pending} onClick={() => void handleLogout(close)} role="menuitem" type="button">
            {pending ? dictionary.common.loading : dictionary.common.signOut}
          </button>
        </>
      )}
    </PopupMenu>
  );
}
