# 認証とIDの境界

[English](identity.md) | [日本語](identity.ja.md)

## 役割

対話的な認証はGo APIが担当します。既定では、LoreHubのデータベースに保存したメールアドレスとパスワードの
アカウントを検証します。外部のOIDCプロバイダーにログインを委譲することもでき、その場合APIはOIDC relying party
として動作します。どちらの場合も、ブラウザが持つのはAPIが発行した推測不能なsession cookieだけで、access token
は渡しません。

```text
Browser
  └─ Next.js web (same-origin proxy, opaque session cookie)
       └─ Go API
            ├─ 内蔵ログイン: PostgreSQLのusersとuser_passwords
            └─ 外部OIDC (任意): LOREHUB_OIDC_ISSUERに対するAuthorization Code + PKCE
                 └─ Keycloak / ZITADEL / Okta / Entra ID などのブローカー
                      └─ SAML、LDAP、ソーシャルログインを吸収
```

LoreHub本体が話す連携プロトコルはOIDCだけです。SAML、LDAP、各社固有のソーシャルログインは実装しません。
IdPがSAMLやLDAPしか提供しない会社は、KeycloakやZITADELのようなブローカーを立てて`LOREHUB_OIDC_ISSUER`を
そのブローカーに向けます。LoreHubはブローカーの先に何があるかを知る必要がありません。

## IDモデル

利用者は`users`の1行です。ログイン手段は`(issuer, subject)`をキーとする`user_identities`で紐付けます。

- 内蔵パスワードアカウントは固定のissuer `lorehub`とユーザーIDをsubjectに使います。
- 外部OIDCアカウントはプロバイダーのissuer URLと`sub` claimを使います。初回ログイン時にユーザーを自動作成します。

メールアドレスはプロフィール属性で、IDのキーではありません。メールアドレスが同じでも、IDを自動で
リンクすることはありません。

## 内蔵パスワードログイン

- パスワードハッシュはargon2idで、プロフィールとは別の`user_passwords`に保存します。ログインには
  メールアドレスまたはユーザー名を使えます。
- パスワードは12文字以上とし、大文字、小文字、数字、記号を含めます。ユーザー名とメールアドレスは含められません。
- ログインに5回連続で失敗するとアカウントをロックします。待機時間は30秒から始まり、失敗のたびに倍増して
  最大15分になります。存在しないアカウントでも、パスワード誤りと同じ検証時間がかかるようにしています。
- 自己登録は既定で有効です。既存アカウントだけに制限するには`LOREHUB_AUTH_PASSWORD_REGISTRATION=disabled`を
  設定します。
- ログインと登録のリクエストはsame-originのJSON POSTに限定し、外部サイトのフォームから送れる形のリクエストは
  拒否します。
- パスワード変更には現在のパスワードと、CSRF tokenを含む有効なsessionが必要です。変更するとその利用者の他の
  sessionをすべて失効させます。
- パスワードを忘れた場合は、メールで届く使い切りのリンクから再設定します。リンクは60分で無効になります。
  再設定メールは通知メールと同じSMTP設定を使い、メール送信が未設定の場合はログイン画面に再設定の入り口を
  表示しません。再設定が完了すると、そのアカウントのsessionをすべて失効させます。
- 内蔵ストアはパスワード履歴の再利用チェックとメールアドレス確認を行いません。これらが必要なインストールでは、
  それらを強制できる外部OIDCプロバイダーにログインを委譲してください。

## 外部OIDCプロバイダー

次の値を設定すると、対話的ログインを外部に委譲します。

```env
LOREHUB_OIDC_ISSUER=https://auth.example.com
LOREHUB_OIDC_AUDIENCE=lorehub-api
LOREHUB_OIDC_CLIENT_ID=lorehub-web
LOREHUB_OIDC_CLIENT_SECRET=...
LOREHUB_OIDC_REDIRECT_URL=https://lorehub.example.com/auth/callback
```

APIはissuerからOIDC discoveryを読み、PKCE S256付きのAuthorization Code Flowを実行します。ID tokenはclient ID、
bearer access tokenは`LOREHUB_OIDC_AUDIENCE`に対して検証します。sessionの扱いは内蔵ログインと同じで、
サーバー側に保持します。

issuerが複数プロバイダーを束ねるブローカーの場合、ログイン画面から特定のプロバイダーへ直接遷移できます。
そのヒントに使うqueryパラメーター名の既定値はKeycloakの`kc_idp_hint`で、`LOREHUB_OIDC_IDP_HINT_PARAM`で
変更できます。

OIDCプロバイダーを設定すると、内蔵パスワードログインは既定で無効になります。`LOREHUB_AUTH_PASSWORD=enabled`を
設定すると両方を並行して有効にできます。同梱のKeycloak Composeプロファイルは、ソーシャルログインと参照実装の
ブローカーとして引き続き使えます。[Keycloak運用ガイド](../operations/keycloak.ja.md)を参照してください。

## 認証mode

Composeの既定値は`LOREHUB_AUTH_MODE=interactive`です。内蔵ログイン、外部OIDC、またはその両方でブラウザからの
ログインを有効にします。bearer tokenだけを送るAPI clientでは`LOREHUB_AUTH_MODE=bearer`を使います。このmodeには
OIDCのissuerとaudienceが必要です。`LOREHUB_AUTH_MODE=disabled`は、ログインを使わないローカル開発専用です。

これらのmodeはLoreHubアプリケーションに適用されます。Lore Serverは、APIが発行する短命JWTを引き続き検証します。
JWTは`urc-{repository_id}`に限定されます。Lore UCS認証、JWKS、鍵の更新、TLS、保護branch hookについては、
[リポジトリ認可ガイド](../operations/control-plane-authorization.ja.md)を参照してください。
