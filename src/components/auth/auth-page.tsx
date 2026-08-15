import { ArrowRight, KeyRound, ShieldCheck } from "lucide-react";
import Link from "next/link";

import type { Dictionary } from "@/i18n";
import type { AuthProviderDirectory } from "@/lib/api-types";
import { providerLoginUrl, safeReturnTo } from "@/lib/routes";

import styles from "./auth-page.module.css";
import { PasswordForm } from "./password-form";

type AuthPageProps = {
  dictionary: Dictionary;
  locale: "en" | "ja";
  directory: AuthProviderDirectory | null;
  register: boolean;
  returnTo: string;
};

export function AuthPage({ dictionary, locale, directory, register, returnTo }: AuthPageProps) {
  const copy = register ? dictionary.authPage.registerDescription : dictionary.authPage.signInDescription;
  const title = register ? dictionary.authPage.registerTitle : dictionary.authPage.signInTitle;
  const alternateHref = register
    ? `/${locale}/auth/login?return_to=${encodeURIComponent(safeReturnTo(returnTo))}`
    : `/${locale}/auth/register?return_to=${encodeURIComponent(safeReturnTo(returnTo))}`;
  const alternateText = register ? dictionary.authPage.alreadyHaveAccount : dictionary.authPage.needAccount;
  return (
    <div className={styles.page} data-auth-page>
      <section aria-labelledby="auth-title" className={styles.card}>
        <div className={styles.brand}>
          <span aria-hidden="true" className={styles.mark}>
            L
          </span>
          <span className="visually-hidden">{dictionary.common.productName}</span>
        </div>
        <div className={styles.heading}>
          <h1 id="auth-title">{title}</h1>
          <p className="visually-hidden">{copy}</p>
        </div>
        {directory ? (
          <AuthOptions
            dictionary={dictionary}
            directory={directory}
            locale={locale}
            register={register}
            returnTo={returnTo}
          />
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

type AuthOptionsProps = {
  dictionary: Dictionary;
  directory: AuthProviderDirectory;
  locale: "en" | "ja";
  register: boolean;
  returnTo: string;
};

function AuthOptions({ dictionary, directory, locale, register, returnTo }: AuthOptionsProps) {
  const formProvider = directory.providers.find((provider) => provider.kind === "form") ?? null;
  const redirectProviders = directory.providers.filter((provider) => provider.kind === "redirect");
  const showForm = formProvider !== null && (!register || directory.passwordRegistration);
  const registrationClosed = register && formProvider !== null && !directory.passwordRegistration;
  return (
    <div aria-label={dictionary.authPage.configuredNote} className={styles.providers}>
      {showForm ? (
        <PasswordForm
          copy={{
            identifierLabel: dictionary.authPage.form.identifierLabel,
            usernameLabel: dictionary.authPage.form.usernameLabel,
            emailLabel: dictionary.authPage.form.emailLabel,
            passwordLabel: dictionary.authPage.form.passwordLabel,
            passwordRequirements: dictionary.authPage.form.passwordRequirements,
            submitSignIn: dictionary.authPage.form.submitSignIn,
            submitRegister: dictionary.authPage.form.submitRegister,
            errors: dictionary.authPage.form.errors,
          }}
          locale={locale}
          register={register}
          returnTo={returnTo}
        />
      ) : null}
      {registrationClosed ? (
        <p className={styles.note} role="alert">
          {dictionary.authPage.registrationClosed}
        </p>
      ) : null}
      {showForm && redirectProviders.length > 0 ? (
        <p className={styles.divider}>{dictionary.authPage.orDivider}</p>
      ) : null}
      {redirectProviders.map((provider) => (
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
  );
}
