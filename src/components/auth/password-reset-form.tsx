"use client";

import { useState } from "react";

import { postPasswordReset, postPasswordResetRequest } from "@/lib/auth-client";

import styles from "./password-form.module.css";

export type PasswordResetCopy = {
  emailLabel: string;
  newPasswordLabel: string;
  passwordRequirements: string;
  submitRequest: string;
  submitReset: string;
  sentTitle: string;
  sentBody: string;
  doneTitle: string;
  doneBody: string;
  backToSignIn: string;
  errors: {
    invalid_reset_token: string;
    weak_password: string;
    reset_unavailable: string;
    unavailable: string;
  };
};

type PasswordResetFormProps = {
  copy: PasswordResetCopy;
  locale: "en" | "ja";
  token: string | null;
};

function errorMessage(copy: PasswordResetCopy, code: string | null): string {
  switch (code) {
    case "invalid_reset_token":
      return copy.errors.invalid_reset_token;
    case "weak_password":
      return copy.errors.weak_password;
    case "reset_unavailable":
      return copy.errors.reset_unavailable;
    default:
      return copy.errors.unavailable;
  }
}

export function PasswordResetForm({ copy, locale, token }: PasswordResetFormProps) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [finished, setFinished] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) {
      return;
    }
    setSubmitting(true);
    setError(null);
    const result = token
      ? await postPasswordReset({ token, newPassword: password })
      : await postPasswordResetRequest({ email });
    if (result.ok) {
      setFinished(true);
      return;
    }
    setError(errorMessage(copy, result.code));
    setSubmitting(false);
  }

  if (finished) {
    return (
      <div className={styles.form} role="status">
        <strong>{token ? copy.doneTitle : copy.sentTitle}</strong>
        <p className={styles.requirements}>{token ? copy.doneBody : copy.sentBody}</p>
        <a className={`${styles.submit} ${styles.linkButton}`} href={`/${locale}/auth/login`}>
          {copy.backToSignIn}
        </a>
      </div>
    );
  }

  return (
    <form className={styles.form} onSubmit={submit}>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      {token ? (
        <>
          <label className={styles.field}>
            <span>{copy.newPasswordLabel}</span>
            <input
              autoComplete="new-password"
              maxLength={512}
              minLength={12}
              name="newPassword"
              onChange={(event) => setPassword(event.target.value)}
              required
              type="password"
              value={password}
            />
          </label>
          <p className={styles.requirements}>{copy.passwordRequirements}</p>
        </>
      ) : (
        <label className={styles.field}>
          <span>{copy.emailLabel}</span>
          <input
            autoComplete="email"
            maxLength={320}
            name="email"
            onChange={(event) => setEmail(event.target.value)}
            required
            type="email"
            value={email}
          />
        </label>
      )}
      <button className={styles.submit} disabled={submitting} type="submit">
        {token ? copy.submitReset : copy.submitRequest}
      </button>
    </form>
  );
}
