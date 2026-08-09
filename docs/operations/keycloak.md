# Keycloak 運用ガイド

LoreHubの認証・ID管理は自己ホストのKeycloakに委譲します。LoreHub本体はユーザーのパスワードを保存せず、
Keycloakがローカル認証とソーシャルIDプロバイダーのブローカーを担当します。この文書はローカル起動から
本番運用までの手順と注意点をまとめます。

## 構成の概要

- Keycloakは専用のPostgreSQL（`keycloak-postgres`）を使います。LoreHubアプリケーションのPostgreSQLとは
  完全に分離されており、認証情報、スキーマ、バックアップは独立しています。
- Keycloakは本番モード（`start`、`start-dev` ではない）で起動します。ローカルはHTTPを明示的に有効化し、
  `hostname` を固定してトークンのissuerを安定させます。
- レルム `lorehub` とクライアントは `infra/keycloak/realm-lorehub.json` から初回起動時に取り込まれます。
  ソーシャルIDプロバイダーは `infra/keycloak/bootstrap.sh` が資格情報の有無に応じて条件付きでプロビジョニング
  します。資格情報のないプロバイダーはログイン画面に壊れたボタンとして表示されません。

## 初回セットアップ

Docker Composeは永続化すべきシークレットを安全に自動生成できないため、専用スクリプトで生成します。
生成された値はgit管理外の `.env` に書き込まれ、ログには一切出力されません。

```bash
scripts/setup-keycloak-secrets.sh
docker compose -f infra/compose.yaml up --build
```

起動後のエンドポイント:

- Keycloak管理コンソール: <http://keycloak.localhost:8280/admin/master/console>
  （ユーザー名 `admin`、パスワードは `.env` の `KEYCLOAK_ADMIN_PASSWORD`）
- LoreHubレルム: <http://keycloak.localhost:8280/realms/lorehub>
- OIDC discovery: <http://keycloak.localhost:8280/realms/lorehub/.well-known/openid-configuration>

`scripts/setup-keycloak-secrets.sh` は `POSTGRES_PASSWORD`、`KEYCLOAK_ADMIN_PASSWORD`、
`KEYCLOAK_DB_PASSWORD`、`LOREHUB_OIDC_CLIENT_SECRET`、`LOREHUB_AUTH_SECRET` を生成します。
既存の空欄は埋めますが、既存の値は保持します。ローテーションするときだけ `--force` を使います。
ファイルの権限は `0600` に固定され、値は端末やログへ表示されません。

## OIDCクライアント

レルムには2つのクライアントが定義されています。

| クライアント  | 種類         | 用途                                               |
| ------------- | ------------ | -------------------------------------------------- |
| `lorehub-web` | confidential | Next.js webアプリ。Authorization Code + PKCE。     |
| `lorehub-api` | bearer-only  | Go APIのリソースサーバー。トークンのaudience検証。 |

`lorehub-web` は `lorehub-api` をトークンの `aud` に含めるようマッピングされています。Go APIは
`LOREHUB_OIDC_AUDIENCE=lorehub-api` でトークンを検証します。

### ローカルログインとパスワードポリシー

自己登録とメールアドレスでのログインを有効にしています。ブートストラップ後のパスワードポリシーは、12文字以上、
大文字・小文字・数字・記号を各1つ以上、ユーザー名とメールアドレスと同じ値を禁止し、過去3世代の再利用を禁止します。
総当たり対策は5回の失敗から待機を始め、待機時間は最大900秒です。

パスワードのハッシュとメールアドレスはKeycloakだけが保持します。LoreHubのPostgreSQLにはパスワードを保存せず、
この構成ではサンプルユーザーも初期パスワードも作成しません。

### リダイレクトとログアウトのURL

初回インポートにはローカル開発用URLが入っています。`keycloak-bootstrap` は起動のたびに環境変数から
クライアント設定を更新するため、本番URLへの変更やクライアントシークレットのローテーションに手作業は不要です。

- rootUrl: `http://localhost:3000`
- リダイレクトURI: `http://localhost:3000/auth/callback` の1件だけ
- ログアウト後リダイレクトURI: `http://localhost:3000/` の1件だけ
- Web Origin: `http://localhost:3000`

本番では `.env` の `LOREHUB_PUBLIC_ORIGIN`、`LOREHUB_OIDC_REDIRECT_URL`、
`LOREHUB_OIDC_LOGOUT_REDIRECT_URL` を同じ公開HTTPSサイトに合わせます。`KEYCLOAK_HOSTNAME` と
`LOREHUB_OIDC_ISSUER` は公開HTTPSのKeycloak URLに合わせます。

## Go APIとOIDC issuerの関係

Go APIは `LOREHUB_OIDC_ISSUER` を使ってOIDC discoveryを取得し、トークンのissuer・audience・署名・有効期限を
検証します。ブラウザログインを使うCompose既定値は `LOREHUB_AUTH_MODE=interactive`、issuerは
`http://keycloak.localhost:8280/realms/lorehub`、audienceは `lorehub-api`、クライアントIDは `lorehub-web` です。
APIはKeycloakのhealthcheckとbootstrap完了を待ってから起動します。

`LOREHUB_AUTH_MODE=disabled` を明示すると、APIの認証を無効にした既存の開発挙動を選べます。既存のBearer
クライアントだけを使う場合は `LOREHUB_AUTH_MODE=bearer` とissuer、audienceを設定します。Keycloakを使わない
場合は、APIをホストで起動するか、`LOREHUB_AUTH_MODE=disabled docker compose -f infra/compose.yaml run --rm
--no-deps api` のようにComposeの依存サービスを外してください。

### ローカルDockerでのdiscovery到達性の注意

既定のCompose構成ではissuerを `http://keycloak.localhost:8280/realms/lorehub` にします。ブラウザは
`keycloak.localhost` をホストのループバックとして解決し、APIコンテナだけが `extra_hosts` の
`keycloak.localhost:host-gateway` を使って同じ公開ポートへ到達します。ブラウザとAPIが同じissuerを使うため、
discoveryとJWKSのURLが一致します。

本番や別のネットワーク構成では、次の条件を満たしてください。

1. issuerがブラウザとAPIの両方から到達できること。
2. OIDC discoveryが返すissuerと、環境変数の `LOREHUB_OIDC_ISSUER` が完全一致すること。
3. 逆プロキシを使う場合、外部のTLS、Host、Forwardedヘッダーを正しく渡すこと。

Keycloakをコンテナ内DNS名だけで公開する設定は、ブラウザがその名前を解決できないため避けてください。

## ソーシャルIDプロバイダー

各プロバイダーは `LOREHUB_IDP_<PROVIDER>_CLIENT_ID` と `LOREHUB_IDP_<PROVIDER>_CLIENT_SECRET` の両方が
設定されているときだけ有効になります。起動後にどちらかを削除すると、既存のプロバイダーは無効化されるため、
ログイン画面に壊れたボタンが残りません。資格情報を戻せば、設定を更新して再び有効になります。

プロバイダーの資格情報は各開発者コンソールで取得してください。コールバックURLはKeycloak管理コンソールの
当該プロバイダー画面に表示される `Redirect URI` をそのまま使います。形式は次の通りです。

```
http://keycloak.localhost:8280/realms/lorehub/broker/<alias>/callback
```

本番では `http://keycloak.localhost:8280` を公開HTTPSのKeycloak URLに置き換えてください。

### Google

- コンソール: Google Cloud Console > APIs & Services > Credentials > OAuth client ID
- アプリケーション種別: Web application
- 承認済みリダイレクトURI:
  `http://keycloak.localhost:8280/realms/lorehub/broker/google/callback`
- スコープ: `openid email profile`（Keycloak既定）
- 任意: `LOREHUB_IDP_GOOGLE_HOSTED_DOMAIN` でGoogle Workspaceドメインに制限できます。

### GitHub

- コンソール: GitHub > Settings > Developer settings > OAuth Apps > New OAuth App
- Authorization callback URL:
  `http://keycloak.localhost:8280/realms/lorehub/broker/github/callback`
- スコープ: `user:email`（Keycloak既定）

### Facebook

- コンソール: Meta for Developers > Apps > 対象アプリ > Facebook Login > Settings
- Valid OAuth Redirect URIs:
  `http://keycloak.localhost:8280/realms/lorehub/broker/facebook/callback`
- スコープ: `email`（Keycloak既定）

### X（旧Twitter）

Keycloak 26.7では組み込みのTwitterブローカーが非推奨（`twitter-broker` 機能フラグの背後に退避、27.0で削除）
となっており、OAuth 1.0aと未保守のtwitter4jに依存しています。最新のKeycloakとXの公式ドキュメントに従い、
XのOAuth 2.0エンドポイントに対して汎用OAuth v2プロバイダーを設定します。

- コンソール: X Developer Portal > App > User authentication settings > OAuth 2.0
- App type: Web App（Confidential Client）
- Callback URI / Redirect URL:
  `http://keycloak.localhost:8280/realms/lorehub/broker/x/callback`
- Type of App: Confidential Client（Client Secretが発行されます）
- スコープ: `users.read users.email`
- 使用するエンドポイント:
  - Authorization: `https://x.com/i/oauth2/authorize`
  - Token: `https://api.x.com/2/oauth2/token`
  - UserInfo: `https://api.x.com/2/users/me?user.fields=confirmed_email`
- UserInfo応答は `{"data":{"id","name","username","confirmed_email"}}` の形なので、claim名にドット記法
  （`data.id`、`data.username`、`data.name`、`data.confirmed_email`）を使い、Keycloakのclaimリゾルバで
  ネストを展開します。
- PKCE（S256）を有効化しています。

`bootstrap.sh` はこれらのエンドポイントとclaimマッピングを実際の値で設定します。プレースホルダーではありません。

## アカウント連携のリスク

ソーシャルプロバイダーからのメールは信頼しない設定（`trustEmail=false`）にしています。これは、未検証の
ソーシャルメールで既存アカウントを乗っ取るリスクを減らすためです。初回ログイン時の `firstBrokerLogin` フローで
既存メールとの重複が検出された場合、ユーザーは既存アカウントへのリンクを確認する必要があります。

メールアドレスの重複とアカウントリンクの挙動は、レルム設定の「Identity Provider Mappers」と
「First Broker Login Flow」で調整できます。本番では、リンクを自動承認せず、確認ステップを維持してください。

## メール検証、パスワードリセット、SMTP

開発環境では `LOREHUB_VERIFY_EMAIL=false` が既定です。SMTPなしで自己登録したユーザーがそのまま
メールアドレスとパスワードでログインできるため、ローカルでメール配送サービスは必要ありません。
パスワードリセットを使うには、開発環境でもSMTPを設定してください。

本番では、次の値をすべて設定してから起動します。

- `LOREHUB_ENV=production`
- `LOREHUB_VERIFY_EMAIL=true`
- `KEYCLOAK_SMTP_HOST`、`KEYCLOAK_SMTP_PORT`、`KEYCLOAK_SMTP_FROM`
- `KEYCLOAK_SMTP_AUTH`。`true` の場合は `KEYCLOAK_SMTP_USER` と `KEYCLOAK_SMTP_PASSWORD` も必要です。
- `KEYCLOAK_SMTP_STARTTLS` または `KEYCLOAK_SMTP_SSL` のどちらか一方を `true` にします。
- 必要に応じて `KEYCLOAK_SMTP_FROM_DISPLAY_NAME`、`KEYCLOAK_SMTP_REPLY_TO`、
  `KEYCLOAK_SMTP_REPLY_TO_DISPLAY_NAME` を設定します。

`keycloak-bootstrap` は本番で検証を無効にしたまま起動すること、またはSMTP設定が欠けたまま検証を有効に
することを拒否します。検証が有効なときは、レルムの `verifyEmail` とSMTP設定を起動のたびに更新します。
SMTPパスワードは `.env` から環境変数として渡され、ブートストラップのログには値を出しません。

## バックアップと移行

- `keycloak-postgres-data` ボリュームがKeycloakの全状態を保持します。LoreHubアプリケーションDB
  （`postgres-data`）とは別にバックアップしてください。
- バックアップ例:
  ```bash
  docker compose -f infra/compose.yaml exec keycloak-postgres \
    pg_dump -U keycloak keycloak > keycloak-backup.sql
  ```
- Keycloakバージョンアップ時は、先に `pg_dump` でDBをバックアップし、Keycloakのアップグレードガイドに
  従ってください。バージョンを更新するときは `infra/keycloak/Dockerfile` の `FROM` 行と
  `infra/compose.yaml` の `keycloak-bootstrap` サービスのイメージタグを、同じリリース版に揃えます。
- レルムJSONの変更は、既存レルムがある場合は初回取り込みがスキップされるため、`kcadm` で個別に適用するか、
  一時的に別レルムへ取り込んで検証してください。

## 本番のTLSとリバースプロキシ

本番ではHTTPを直接公開せず、TLS終端のリバースプロキシ（nginx、Caddy、Cloudflareなど）の背後に置きます。

- `KEYCLOAK_HOSTNAME` を公開HTTPS URLに設定（例: `https://auth.lorehub.example`）。
- Composeの `--proxy-headers=forwarded` に合わせ、プロキシは標準の `Forwarded` ヘッダーを正しく設定します。
  別のヘッダー形式を使う場合は、Keycloakの設定とプロキシの設定を同じ方式に揃えてください。
- Keycloakの公開ポートをインターネットへ直接公開せず、プロキシからだけ到達できるネットワークに置きます。
- `LOREHUB_PUBLIC_ORIGIN`、`LOREHUB_OIDC_REDIRECT_URL`、`LOREHUB_OIDC_LOGOUT_REDIRECT_URL` も
  HTTPSへ変更し、APIのCookie設定 `LOREHUB_SESSION_COOKIE_SECURE=true` を明示します。
- レルムの `sslRequired=external` はローカルのループバック接続を許可し、外部アクセスにはTLSを要求します。
- 管理コンソールは `KEYCLOAK_ADMIN_USERNAME` / `KEYCLOAK_ADMIN_PASSWORD` を初回起動時に作成します。
  本番では初回起動後にパスワードを変更し、管理コンソールへのアクセスをネットワークまたはVPNで制限してください。
