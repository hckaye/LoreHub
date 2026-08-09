import { ArrowRight, KeyRound, ShieldCheck } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { AuthProvider } from "@/lib/api-types";
import { providerLoginUrl, safeReturnTo } from "@/lib/routes";

import styles from "./auth-page.module.css";

type AuthPageProps = {
  dictionary: Dictionary;
  locale: "en" | "ja";
  providers: AuthProvider[] | null;
  register: boolean;
  returnTo: string;
};

export function AuthPage({ dictionary, locale, providers, register, returnTo }: AuthPageProps) {
  const copy = register ? dictionary.authPage.registerDescription : dictionary.authPage.signInDescription;
  const title = register ? dictionary.authPage.registerTitle : dictionary.authPage.signInTitle;
  const alternateHref = register
    ? `/${locale}/auth/login?return_to=${encodeURIComponent(safeReturnTo(returnTo))}`
    : `/${locale}/auth/register?return_to=${encodeURIComponent(safeReturnTo(returnTo))}`;
  const alternateText = register ? dictionary.authPage.alreadyHaveAccount : dictionary.authPage.needAccount;
  return (
    <div className={styles.page}>
      <section aria-labelledby="auth-title" className={styles.card}>
        <div className={styles.brand}>
          <span aria-hidden="true" className={styles.mark}>
            L
          </span>
          <span>{dictionary.common.productName}</span>
        </div>
        <div className={styles.heading}>
          <p className={styles.eyebrow}>{register ? dictionary.common.signUp : dictionary.common.signIn}</p>
          <h1 id="auth-title">{title}</h1>
          <p>{copy}</p>
        </div>
        {providers ? (
          <div aria-label={dictionary.authPage.configuredNote} className={styles.providers}>
            {providers.map((provider) => (
              <Link
                className={`${styles.provider} ${provider.id === "password" ? styles.password : ""}`}
                href={providerLoginUrl(returnTo, provider.id, register)}
                key={provider.id}
              >
                {provider.id === "password" ? (
                  <KeyRound aria-hidden="true" size={18} />
                ) : (
                  <span aria-hidden="true" className={styles.providerMark}>
                    {provider.id === "x" ? "X" : provider.id.slice(0, 1).toUpperCase()}
                  </span>
                )}
                <span>
                  {dictionary.authPage.continueWith.replace("{provider}", dictionary.authPage.providers[provider.id])}
                </span>
                <ArrowRight aria-hidden="true" size={16} />
              </Link>
            ))}
          </div>
        ) : (
          <div className={styles.unavailable} role="alert">
            <ShieldCheck aria-hidden="true" size={19} />
            <div>
              <strong>{dictionary.authPage.unavailableTitle}</strong>
              <p>{dictionary.authPage.unavailableBody}</p>
            </div>
          </div>
        )}
        <p className={styles.note}>{dictionary.authPage.configuredNote}</p>
        <div className={styles.alternate}>
          <span>{alternateText}</span>
          <Link href={alternateHref}>{register ? dictionary.common.signIn : dictionary.common.signUp}</Link>
        </div>
        <Link className={styles.homeLink} href={`/${locale}`}>
          {dictionary.authPage.backToHome}
        </Link>
      </section>
    </div>
  );
}
