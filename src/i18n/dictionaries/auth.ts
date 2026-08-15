function concat(...parts: string[]): string {
  return parts.join("");
}

export const auth = {
  en: {
    signInTitle: "Sign in to LoreHub",
    signInDescription: "Sign in with your email address and password or a configured identity provider.",
    registerTitle: "Create a LoreHub account",
    registerDescription:
      "Create an account with an email address and password or through a configured identity provider.",
    alreadyHaveAccount: "Already have an account?",
    needAccount: "Need an account?",
    continueWith: "Continue with {provider}",
    orDivider: "or",
    registrationClosed: "Self-registration is disabled on this installation. Ask an administrator for an account.",
    configuredNote: "The administrator of this installation configures the available sign-in methods.",
    unavailableTitle: "Sign-in is unavailable",
    unavailableBody: "The authentication service is not available. Try again when it has recovered.",
    backToHome: "Back to home",
    form: {
      identifierLabel: "Email or username",
      usernameLabel: "Username",
      emailLabel: "Email",
      passwordLabel: "Password",
      passwordRequirements: "At least 12 characters with uppercase and lowercase letters, a number, and a symbol.",
      submitSignIn: "Sign in",
      submitRegister: "Create account",
      forgotPassword: "Forgot your password?",
      errors: {
        invalid_credentials: "The email address, username, or password is incorrect.",
        account_locked: "Too many failed attempts locked the account temporarily. Try again later.",
        username_taken: "The username is already taken.",
        email_taken: "The email address is already registered.",
        weak_password: "The password does not meet the requirements below.",
        invalid_username: "Usernames use 2 to 63 lowercase letters, numbers, and hyphens.",
        invalid_email: "The email address is invalid.",
        registration_disabled: "Self-registration is disabled on this installation.",
        unavailable: "Sign-in is temporarily unavailable. Try again in a moment.",
      },
    },
    reset: {
      title: "Reset your password",
      requestDescription: "Enter your account email address to receive a password reset link.",
      confirmDescription: "Choose a new password for your account.",
      newPasswordLabel: "New password",
      submitRequest: "Send reset link",
      submitReset: "Set new password",
      sentTitle: "Check your email",
      sentBody: "If the address belongs to an account, a reset link is on its way. The link expires in 60 minutes.",
      doneTitle: "Password updated",
      doneBody: "Your password has been changed and other sessions were signed out. Sign in with the new password.",
      backToSignIn: "Go to sign-in",
      errors: {
        invalid_reset_token: "The reset link is invalid or has expired. Request a new one.",
        reset_unavailable: "Password reset email is not configured on this installation. Ask an administrator.",
      },
    },
    providers: {
      password: "email and password",
      sso: "single sign-on",
      google: "Google",
      github: "GitHub",
      facebook: "Facebook",
      x: "X",
    },
  },
  ja: {
    signInTitle: "LoreHubにログイン",
    // prettier-ignore
    signInDescription: concat(
    "メールアドレスとパスワード、または設定済みのIDプロバイダーで",
    "ログインします。",
  ),
    registerTitle: "LoreHubアカウントを作成",
    // prettier-ignore
    registerDescription: concat(
    "メールアドレスとパスワード、または設定済みのIDプロバイダーで",
    "アカウントを作成します。",
  ),
    alreadyHaveAccount: "すでにアカウントをお持ちですか？",
    needAccount: "アカウントが必要ですか？",
    continueWith: "{provider}で続ける",
    orDivider: "または",
    // prettier-ignore
    registrationClosed: concat(
    "このLoreHubでは新規登録が無効になっています。",
    "アカウントの発行は管理者に依頼してください。",
  ),
    configuredNote: "利用できるログイン方法は、このLoreHubの管理者が設定します。",
    unavailableTitle: "ログインを利用できません",
    unavailableBody: "認証サービスを利用できません。復旧してから再試行してください。",
    backToHome: "ホームに戻る",
    form: {
      identifierLabel: "メールアドレスまたはユーザー名",
      usernameLabel: "ユーザー名",
      emailLabel: "メールアドレス",
      passwordLabel: "パスワード",
      // prettier-ignore
      passwordRequirements: concat(
      "12文字以上で、大文字・小文字・数字・記号を",
      "それぞれ含めてください。",
    ),
      submitSignIn: "ログイン",
      submitRegister: "アカウントを作成",
      forgotPassword: "パスワードをお忘れですか？",
      errors: {
        invalid_credentials: "メールアドレス、ユーザー名、またはパスワードが正しくありません。",
        // prettier-ignore
        account_locked: concat(
        "ログイン失敗が続いたため、アカウントを一時的にロックしました。",
        "しばらく待ってから再試行してください。",
      ),
        username_taken: "このユーザー名はすでに使われています。",
        email_taken: "このメールアドレスはすでに登録されています。",
        weak_password: "パスワードが下記の要件を満たしていません。",
        invalid_username: "ユーザー名には2〜63文字の小文字英数字とハイフンを使います。",
        invalid_email: "メールアドレスが正しくありません。",
        registration_disabled: "このLoreHubでは新規登録が無効になっています。",
        unavailable: "現在ログインを利用できません。しばらく待ってから再試行してください。",
      },
    },
    reset: {
      title: "パスワードの再設定",
      requestDescription: "アカウントのメールアドレスを入力すると、再設定用のリンクを送信します。",
      confirmDescription: "新しいパスワードを設定します。",
      newPasswordLabel: "新しいパスワード",
      submitRequest: "再設定リンクを送信",
      submitReset: "パスワードを設定",
      sentTitle: "メールを確認してください",
      // prettier-ignore
      sentBody: concat(
      "入力したメールアドレスのアカウントがあれば、再設定リンクを送信しました。",
      "リンクは60分で無効になります。",
    ),
      doneTitle: "パスワードを変更しました",
      // prettier-ignore
      doneBody: concat(
      "パスワードを変更し、他のログイン状態をすべて解除しました。",
      "新しいパスワードでログインしてください。",
    ),
      backToSignIn: "ログイン画面へ",
      errors: {
        invalid_reset_token: "再設定リンクが無効か、期限切れです。もう一度リンクを取得してください。",
        // prettier-ignore
        reset_unavailable: concat(
        "このLoreHubではパスワード再設定メールが設定されていません。",
        "管理者に問い合わせてください。",
      ),
      },
    },
    providers: {
      password: "メールアドレスとパスワード",
      sso: "シングルサインオン",
      google: "Googleアカウント",
      github: "GitHubアカウント",
      facebook: "Facebookアカウント",
      x: "Xアカウント",
    },
  },
};
