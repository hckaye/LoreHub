"use client";

import { CircleUserRound } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

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

  async function handleLogout() {
    if (!session.csrfToken) {
      setError(true);
      return;
    }
    setError(false);
    setPending(true);
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
    <details className={styles.menu}>
      <summary className={styles.summary}>
        <span aria-hidden="true" className={styles.avatar}>
          {user.displayName.slice(0, 1).toUpperCase()}
        </span>
        <span className={styles.name}>{user.displayName}</span>
      </summary>
      <div className={styles.dropdown}>
        <div className={styles.identity}>
          <CircleUserRound aria-hidden="true" size={18} />
          <div>
            <strong>{user.displayName}</strong>
            <span>@{user.username}</span>
          </div>
        </div>
        <Link href={`/${locale}/profile`}>{dictionary.common.profile}</Link>
        <Link href={`/${locale}/notifications`}>{dictionary.common.notifications}</Link>
        <Link href={`/${locale}/organizations`}>{dictionary.common.organizations}</Link>
        <Link href={`/${locale}/settings`}>{dictionary.common.accountSettings}</Link>
        {error && <p className={styles.error}>{dictionary.auth.logoutFailed}</p>}
        <button disabled={pending} onClick={handleLogout} type="button">
          {pending ? dictionary.common.loading : dictionary.common.signOut}
        </button>
      </div>
    </details>
  );
}
