"use client";

import { useState } from "react";

import { postPasswordLogin, postPasswordRegister } from "@/lib/auth-client";
import { safeReturnTo } from "@/lib/routes";

import styles from "./password-form.module.css";

export type PasswordFormCopy = {
  identifierLabel: string;
  usernameLabel: string;
  emailLabel: string;
  passwordLabel: string;
  passwordRequirements: string;
  submitSignIn: string;
  submitRegister: string;
  forgotPassword: string;
  errors: {
    invalid_credentials: string;
    account_locked: string;
    username_taken: string;
    email_taken: string;
    weak_password: string;
    invalid_username: string;
    invalid_email: string;
    registration_disabled: string;
    unavailable: string;
  };
};

function errorMessage(copy: PasswordFormCopy, code: string | null): string {
  switch (code) {
    case "invalid_credentials":
      return copy.errors.invalid_credentials;
    case "account_locked":
      return copy.errors.account_locked;
    case "username_taken":
      return copy.errors.username_taken;
    case "email_taken":
      return copy.errors.email_taken;
    case "weak_password":
      return copy.errors.weak_password;
    case "invalid_username":
      return copy.errors.invalid_username;
    case "invalid_email":
      return copy.errors.invalid_email;
    case "registration_disabled":
      return copy.errors.registration_disabled;
    default:
      return copy.errors.unavailable;
  }
}

type PasswordFormProps = {
  copy: PasswordFormCopy;
  locale: "en" | "ja";
  register: boolean;
  resetAvailable: boolean;
  returnTo: string;
};

export function PasswordForm({ copy, locale, register, resetAvailable, returnTo }: PasswordFormProps) {
  const [identifier, setIdentifier] = useState("");
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) {
      return;
    }
    setSubmitting(true);
    setError(null);
    const result = register
      ? await postPasswordRegister({ username, email, password, locale })
      : await postPasswordLogin({ identifier, password });
    if (result.ok) {
      window.location.assign(safeReturnTo(returnTo));
      return;
    }
    setError(errorMessage(copy, result.code));
    setSubmitting(false);
  }

  return (
    <form className={styles.form} onSubmit={submit}>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}
      {register ? (
        <>
          <label className={styles.field}>
            <span>{copy.usernameLabel}</span>
            <input
              autoComplete="username"
              maxLength={63}
              name="username"
              onChange={(event) => setUsername(event.target.value)}
              pattern="[a-z0-9][a-z0-9-]*"
              required
              type="text"
              value={username}
            />
          </label>
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
        </>
      ) : (
        <label className={styles.field}>
          <span>{copy.identifierLabel}</span>
          <input
            autoComplete="username"
            maxLength={320}
            name="identifier"
            onChange={(event) => setIdentifier(event.target.value)}
            required
            type="text"
            value={identifier}
          />
        </label>
      )}
      <label className={styles.field}>
        <span>{copy.passwordLabel}</span>
        <input
          autoComplete={register ? "new-password" : "current-password"}
          maxLength={512}
          minLength={register ? 12 : undefined}
          name="password"
          onChange={(event) => setPassword(event.target.value)}
          required
          type="password"
          value={password}
        />
      </label>
      {register ? <p className={styles.requirements}>{copy.passwordRequirements}</p> : null}
      {!register && resetAvailable ? (
        <p className={styles.forgot}>
          <a href={`/${locale}/auth/reset`}>{copy.forgotPassword}</a>
        </p>
      ) : null}
      <button className={styles.submit} disabled={submitting} type="submit">
        {register ? copy.submitRegister : copy.submitSignIn}
      </button>
    </form>
  );
}
