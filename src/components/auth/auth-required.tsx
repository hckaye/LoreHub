import { LockKeyhole } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { AuthSession } from "@/lib/api-types";
import { loginUrl } from "@/lib/routes";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./auth-required.module.css";

type AuthRequiredProps = {
  dictionary: Dictionary;
  session: AuthSession;
  returnTo: string;
};

export function AuthRequired({ dictionary, session, returnTo }: AuthRequiredProps) {
  if (session.status === "authenticated") {
    return null;
  }
  const isUnavailable = session.status === "unavailable";
  return (
    <div className={styles.wrapper}>
      <FlashNotice
        body={isUnavailable ? dictionary.auth.sessionUnavailableBody : dictionary.auth.requiredBody}
        icon={<LockKeyhole aria-hidden="true" size={18} />}
        title={isUnavailable ? dictionary.auth.sessionUnavailableTitle : dictionary.auth.requiredTitle}
        tone={isUnavailable ? "warning" : "info"}
      />
      {!isUnavailable && (
        <Link className={styles.link} href={loginUrl(returnTo)}>
          {dictionary.auth.loginToContinue}
        </Link>
      )}
    </div>
  );
}
