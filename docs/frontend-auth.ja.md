# Frontendの認証契約

[English](frontend-auth.md) | [日本語](frontend-auth.ja.md)

認証状態はGo APIが管理します。OAuth access tokenはサーバー側sessionに保存し、ブラウザのJavaScriptへ公開しません。

## Session

server renderingを行うlocale layoutは`GET /api/v1/auth/session`を呼び出し、受信したsession cookieをAPIへ転送します。
APIは次のいずれかの形式でJSONを返します。

```json
{
  "authenticated": true,
  "user": {
    "id": "user-id",
    "username": "name",
    "displayName": "Display name",
    "email": "name@example.test",
    "locale": "en"
  },
  "session": {
    "id": "session-id",
    "createdAt": "2026-08-09T00:00:00Z",
    "expiresAt": "2026-09-08T00:00:00Z",
    "lastSeenAt": "2026-08-09T00:00:00Z"
  },
  "csrfToken": "session-bound-token"
}
```

匿名のブラウザには`authenticated: false`を含む成功responseを返します。`401`はsessionの期限切れとして扱い、
その他の失敗では認証serviceを利用できないことを表示します。Frontendのnormalizerは未知のfieldを除去します。
CSRF値はサーバー側sessionに紐付き、そのsessionが有効な間だけ使用できます。OAuth access tokenとは別の値です。

## Same-origin route

`/api/*`と`/auth/*`のApp Router handlerは、実行時にrequestを`LOREHUB_API_URL`へ転送します。ブラウザはsame-originの
pathを使用します。upstream URLは実行時に渡すため、同じWeb imageを異なる環境で利用できます。更新requestでは
`credentials: "include"`を指定し、sessionのCSRF tokenを`X-CSRF-Token`に設定します。

logoutには`POST /auth/logout`を使います。Issue、pull request、organization、repositoryのformは既存のAPI endpointを
呼び出し、serverが返したunauthorized、forbidden、invalid、conflict、unavailableの状態を表示します。

## Loginとregistration

`/{locale}/auth/login`と`/{locale}/auth/register`のログイン画面は`GET /api/v1/auth/providers`を読み、
そのインストールで使える手段を表示します。内蔵のパスワードフォーム（`kind: "form"`）、外部OIDCプロバイダーへの
リンク（`kind: "redirect"`）、またはその両方です。return pathはencode前に検証します。絶対URL、
protocol-relative URL、backslashを使ったpathは`/`へ置き換えます。

パスワードフォームはsame-origin proxy経由で`POST /auth/password/login`と`POST /auth/password/register`へJSONを
送ります。これらのendpointはcontent typeが`application/json`であることと`Origin`の一致を要求し、成功時に
session cookieを設定します。エラーは`invalid_credentials`、`account_locked`、`username_taken`、`email_taken`、
`weak_password`、`registration_disabled`などのproblem codeで返り、フォームが辞書の文言に対応付けます。
provider応答の`passwordRegistration`がfalseの場合、登録画面はフォームの代わりに登録停止の案内を表示します。

外部OIDCへのログインは従来どおり`/auth/login?return_to=<relative-path>`から開始します。アカウント登録は
`prompt=create`を付けます。これはKeycloakなどのOIDCプロバイダーがregistration画面を開くqueryです。OIDC
プロバイダーが未設定の場合、APIは`/auth/login`を`/auth/start`へリダイレクトします。`/auth/start`はlocaleを
判定してログイン画面へ転送する小さなroute handlerです。identity provider側でregistrationが無効な場合は、
そのerrorを画面に表示します。

## Local proxyの設定

`LOREHUB_API_URL`にはNext.js serverから到達できるAPI originを設定します。Composeでは
`http://lorehub.localhost:8080`、host上の開発環境では`http://127.0.0.1:8080`を使用できます。この値はserver専用なので、
`NEXT_PUBLIC_*` prefixを付けません。
