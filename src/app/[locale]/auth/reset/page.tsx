import Link from "next/link";
import { notFound } from "next/navigation";

import { PasswordResetForm } from "@/components/auth/password-reset-form";
import { getDictionary } from "@/i18n";
import { isLocale } from "@/i18n/config";

import styles from "@/components/auth/auth-page.module.css";

type ResetPageProps = {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ token?: string }>;
};

export const dynamic = "force-dynamic";

export default async function ResetPage({ params, searchParams }: ResetPageProps) {
  const { locale: value } = await params;
  if (!isLocale(value)) {
    notFound();
  }
  const [{ token }, dictionary] = await Promise.all([searchParams, getDictionary(value)]);
  const normalizedToken = typeof token === "string" && token.length > 0 && token.length <= 512 ? token : null;
  const reset = dictionary.authPage.reset;
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
          <h1 id="auth-title">{reset.title}</h1>
          <p className="visually-hidden">{normalizedToken ? reset.confirmDescription : reset.requestDescription}</p>
        </div>
        <div className={styles.providers}>
          <PasswordResetForm
            copy={{
              emailLabel: dictionary.authPage.form.emailLabel,
              newPasswordLabel: reset.newPasswordLabel,
              passwordRequirements: dictionary.authPage.form.passwordRequirements,
              submitRequest: reset.submitRequest,
              submitReset: reset.submitReset,
              sentTitle: reset.sentTitle,
              sentBody: reset.sentBody,
              doneTitle: reset.doneTitle,
              doneBody: reset.doneBody,
              backToSignIn: reset.backToSignIn,
              errors: {
                invalid_reset_token: reset.errors.invalid_reset_token,
                weak_password: dictionary.authPage.form.errors.weak_password,
                reset_unavailable: reset.errors.reset_unavailable,
                unavailable: dictionary.authPage.form.errors.unavailable,
              },
            }}
            locale={value}
            token={normalizedToken}
          />
        </div>
        <p className={styles.note}>{normalizedToken ? reset.confirmDescription : reset.requestDescription}</p>
        <div className={styles.alternate}>
          <span>{dictionary.authPage.alreadyHaveAccount}</span>
          <Link href={`/${value}/auth/login`}>{dictionary.common.signIn}</Link>
        </div>
        <Link className={styles.homeLink} href={`/${value}`}>
          {dictionary.authPage.backToHome}
        </Link>
      </section>
    </div>
  );
}
