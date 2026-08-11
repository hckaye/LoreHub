# 認証とIDの境界

[English](identity.md) | [日本語](identity.ja.md)

## 役割

メールアドレスとパスワードによる認証、social identity providerとの連携はKeycloakが担当します。LoreHubの
Go APIとNext.jsアプリケーションはOIDC clientとしてKeycloakを利用し、token claimからユーザー情報を受け取ります。

```text
Browser
  └─ Next.js web (lorehub-web, confidential, Authorization Code + PKCE)
       └─ Keycloak (lorehub realm)
            ├─ Email and password accounts
            ├─ Google, GitHub, Facebook, and X identity providers
            └─ Dedicated PostgreSQL database for authentication data
  └─ Go API (lorehub-api, bearer-only)
       └─ Validates token issuer, audience, signature, and expiry
```

## データの分離

Keycloakは`keycloak-postgres`という専用のPostgreSQL serviceを使います。認証データとLoreHubのアプリケーション
データは、databaseとbackupを分けて管理します。KeycloakからLoreHubのアプリケーションdatabaseには接続できません。

## OIDC client

- `lorehub-web`はAuthorization Code FlowとPKCE S256を使うconfidential clientです。password grantは無効です。
  access tokenのaudienceには`lorehub-api`が含まれます。
- `lorehub-api`はbearer-only resource serverです。access tokenを
  `LOREHUB_OIDC_AUDIENCE=lorehub-api`に対して検証し、ログイン処理は開始しません。

## Tokenとsessionの有効期間

`infra/keycloak/realm-lorehub.json`では次の値を設定しています。

- access token: 300秒
- SSO session: idle 1,800秒、最大28,800秒
- refresh tokenの失効: 有効、再利用なし
- offline sessionのidle timeout: 30日

## パスワードと総当たり対策

- パスワードは12文字以上とし、大文字、小文字、数字、記号を含めます。ユーザー名とメールアドレスは使用できず、
  過去3回のパスワードも再利用できません。
- ログインに5回失敗するとアカウントをロックし、待機時間を最大900秒まで段階的に延長します。
- メールアドレスをログイン名として使い、自己登録を許可します。本番環境ではSMTPを設定し、メール確認を有効にします。

## Social identity providerの設定

social providerの認証情報は環境変数で渡します。`infra/keycloak/bootstrap.sh`はclient IDとclient secretが揃った
providerだけを作成または更新します。認証情報を削除するとproviderを無効化し、元に戻すと再び有効化します。
この処理は繰り返し実行できます。外部access tokenは保存せず、providerのemail claimは確認が取れるまで信頼しません。

Xとの連携には、X公式のOAuth 2.0 endpointを指定した汎用OAuth v2 providerを使います。Keycloak 26.7で非推奨の
Twitter brokerは使いません。設定方法は[Keycloak運用ガイド](../operations/keycloak.ja.md)を参照してください。

## 認証mode

Composeの既定値は`LOREHUB_AUTH_MODE=interactive`です。ブラウザからのログインを有効にし、issuerには
`http://keycloak.localhost:8280/realms/lorehub`を使います。bearer tokenだけを送るAPI clientでは
`LOREHUB_AUTH_MODE=bearer`を使います。`LOREHUB_AUTH_MODE=disabled`は、ログインを使わないローカル開発専用です。

これらのmodeはLoreHubアプリケーションに適用されます。Lore Serverは、APIが発行する短命JWTを引き続き検証します。
JWTは`urc-{repository_id}`に限定されます。Lore UCS認証、JWKS、鍵の更新、TLS、保護branch hookについては、
[リポジトリ認可ガイド](../operations/control-plane-authorization.ja.md)を参照してください。
