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

ログインは`/auth/login?return_to=<relative-path>`から開始します。return pathはencode前に検証します。絶対URL、
protocol-relative URL、backslashを使ったpathは`/`へ置き換えます。

アカウント登録には`/auth/login?return_to=<relative-path>&prompt=create`を使います。`prompt=create`はKeycloakの
registration画面を開くOIDC queryです。identity providerでregistrationが無効な場合は、そのerrorを画面に表示します。

## Local proxyの設定

`LOREHUB_API_URL`にはNext.js serverから到達できるAPI originを設定します。Composeでは
`http://lorehub.localhost:8080`、host上の開発環境では`http://127.0.0.1:8080`を使用できます。この値はserver専用なので、
`NEXT_PUBLIC_*` prefixを付けません。
