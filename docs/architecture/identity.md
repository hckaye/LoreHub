# 認証とIDの境界

## 責務の分離

LoreHub本体はユーザーのパスワードを保存しません。ローカル認証（メール＋パスワード）とソーシャル
IDプロバイダーのブローカーはKeycloakが担当します。LoreHubのGo APIとNext.js webはOIDCクライアントとして
Keycloakを使い、ユーザー情報はOIDCトークンのクレーム経由で受け取ります。

```
Browser
  └─ Next.js web (lorehub-web, confidential, Authorization Code + PKCE)
       └─ Keycloak (lorehub realm)
            ├─ Email/Password（Keycloakが保有）
            ├─ Google / GitHub / Facebook / X（Keycloakがブローカー）
            └─ 専用PostgreSQL（Keycloakの認証データ）
  └─ Go API (lorehub-api, bearer-only)
       └─ トークンのissuer/audience/署名/有効期限を検証
```

## データの分離

Keycloakは専用のPostgreSQL（`keycloak-postgres`）を使い、LoreHubアプリケーションのPostgreSQLとは分離します。
認証データとLoreHubの業務データ（組織、権限、Issue、レビュー、CI、監査）は別々のデータベース・別々の
バックアップ対象です。KeycloakがLoreHubのデータベースを読み書きすることはありません。

## OIDCクライアント

- `lorehub-web`: confidentialクライアント。Authorization Code Flow + PKCE（S256）を使い、
  `directAccessGrants`（パスワードグラント）は無効化しています。トークンの `aud` に `lorehub-api` を含めます。
- `lorehub-api`: bearer-onlyリソースサーバー。ログインフローを持たず、アクセストークンの検証のみを行います。
  Go APIは `LOREHUB_OIDC_AUDIENCE=lorehub-api` で検証します。

## トークンとセッションのライフタイム

レルム設定（`infra/keycloak/realm-lorehub.json`）で次を定義しています。

- アクセストークン有効期限: 300秒（5分）
- SSOセッションidle: 1800秒（30分）、max: 28800秒（8時間）
- リフレッシュトークン失効: 有効、再利用上限0（1回限り）
- オフラインセッションidle: 30日

## パスワードポリシーと総当たり対策

- パスワードポリシー: 12文字以上、大文字・小文字・数字・記号を含む、ユーザー名とメールを含まない、
  過去3世代の再利用禁止。
- 総当たり保護: 有効。失敗5回でロック、待機時間を段階的に延長（最大900秒）。
- ログインIDはメールアドレス。自己登録を許可しています。メール検証は開発では無効、本番ではSMTPを設定したうえで
  有効にします。

## ソーシャルプロバイダーの条件付きプロビジョニング

ソーシャルIDプロバイダーはレルムJSONに固定で書き込まず、`infra/keycloak/bootstrap.sh` が資格情報の有無に
応じて条件付きで作成します。資格情報が両方揃ったプロバイダーだけがログイン画面に表示され、欠落した
プロバイダーは壊れたボタンとして現れません。起動後に資格情報を削除すると既存プロバイダーも無効化され、
戻すと設定を更新して再び有効になります。プロビジョニングは冪等で、外部アクセストークンは保存せず、
外部メールも信頼しません。

X（旧Twitter）は、Keycloak 26.7で非推奨になった組み込みTwitterブローカーを使わず、X公式のOAuth 2.0
エンドポイントに対する汎用OAuth v2プロバイダーとして設定します。詳細は
[Keycloak運用ガイド](../operations/keycloak.md)を参照してください。

## APIのbearer-only無効状態

`LOREHUB_AUTH_MODE=disabled` を明示したとき、Go APIはOIDC検証を無効化した現在の挙動を維持します。既存の
Bearerクライアントだけを使う場合は `LOREHUB_AUTH_MODE=bearer` にします。既定のCompose構成は
`LOREHUB_AUTH_MODE=interactive`、issuerは `http://keycloak.localhost:8280/realms/lorehub` です。

これはLoreのデータ認証を無効にする設定ではありません。ComposeのLore Serverは、APIが発行する短命で
`urc-{repository_id}`に限定されたJWTを検証します。LoreのUCS認証、JWKS、鍵交代、TLS、protected branch hookの
運用は[認可境界の運用ガイド](../operations/control-plane-authorization.md)を参照してください。
