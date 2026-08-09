"use client";

import { CircleAlert, Clock3, ShieldAlert } from "lucide-react";
import { useSearchParams } from "next/navigation";

import type { Dictionary } from "@/i18n";
import type { AuthSession } from "@/lib/api-types";

import { FlashNotice } from "../ui/flash-notice";
import styles from "./auth-notice.module.css";

type AuthNoticeProps = {
  dictionary: Dictionary;
  session: AuthSession;
};

export function AuthNotice({ dictionary, session }: AuthNoticeProps) {
  const searchParams = useSearchParams();
  const providerError = searchParams.get("error");
  if (providerError) {
    return (
      <div className={styles.wrapper}>
        <FlashNotice
          body={dictionary.auth.providerErrorBody}
          icon={<CircleAlert aria-hidden="true" size={18} />}
          title={dictionary.auth.providerErrorTitle}
          tone="error"
        />
      </div>
    );
  }
  if (session.status === "expired") {
    return (
      <div className={styles.wrapper}>
        <FlashNotice
          body={dictionary.auth.sessionExpiredBody}
          icon={<Clock3 aria-hidden="true" size={18} />}
          title={dictionary.auth.sessionExpiredTitle}
          tone="warning"
        />
      </div>
    );
  }
  if (session.status === "unavailable") {
    return (
      <div className={styles.wrapper}>
        <FlashNotice
          body={dictionary.auth.sessionUnavailableBody}
          icon={<ShieldAlert aria-hidden="true" size={18} />}
          title={dictionary.auth.sessionUnavailableTitle}
          tone="warning"
        />
      </div>
    );
  }
  return null;
}
