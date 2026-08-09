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
cp .env.example .env
scripts/setup-keycloak-secrets.sh
docker compose -f infra/compose.yaml up
```

起動後のエンドポイント:

- Keycloak管理コンソール: <http://localhost:8280/admin/master/console>
  （ユーザー名 `admin`、パスワードは `.env` の `KEYCLOAK_ADMIN_PASSWORD`）
- LoreHubレルム: <http://localhost:8280/realms/lorehub>
- OIDC discovery: <http://localhost:8280/realms/lorehub/.well-known/openid-configuration>

`scripts/setup-keycloak-secrets.sh` は `KEYCLOAK_ADMIN_PASSWORD`、`KEYCLOAK_DB_PASSWORD`、
`LOREHUB_OIDC_CLIENT_SECRET` を生成します。既存値を上書きする場合は `--force` を使います。

## OIDCクライアント

レルムには2つのクライアントが定義されています。

| クライアント  | 種類         | 用途                                               |
| ------------- | ------------ | -------------------------------------------------- |
| `lorehub-web` | confidential | Next.js webアプリ。Authorization Code + PKCE。     |
| `lorehub-api` | bearer-only  | Go APIのリソースサーバー。トークンのaudience検証。 |

`lorehub-web` は `lorehub-api` をトークンの `aud` に含めるようマッピングされています。Go APIは
`LOREHUB_OIDC_AUDIENCE=lorehub-api` でトークンを検証します。

### リダイレクトとログアウトのURL

レルムJSONにはローカル開発用のURL（`http://localhost:3000`）が直接設定されています。Keycloakのレルム
インポートはクライアントの `rootUrl`、`redirectUris`、`webOrigins` で環境変数置換をサポートしないため、
concrete URLを使います。

- rootUrl: `http://localhost:3000`
- リダイレクトURI:
  - `/api/auth/callback/lorehub`（rootUrlからの相対）
  - `/auth/callback`（rootUrlからの相対）
  - `http://localhost:3000/api/auth/callback/lorehub`（絶対）
  - `http://localhost:3000/auth/callback`（絶対）
- ログアウト後リダイレクトURI: `http://localhost:3000/*`
- Web Origin: `http://localhost:3000`

本番では `KEYCLOAK_HOSTNAME` を公開HTTPS URLに変更し、`lorehub-web` クライアントの `rootUrl`、
`redirectUris`、`webOrigins`、`post.logout.redirect.uris` をAdmin CLIで更新してください。

```bash
docker compose -f infra/compose.yaml exec keycloak \
  /opt/keycloak/bin/kcadm.sh update clients/lorehub-web -r lorehub \
  -s 'rootUrl=https://app.lorehub.example' \
  -s 'redirectUris=["/api/auth/callback/lorehub","/auth/callback"]' \
  -s 'webOrigins=["https://app.lorehub.example"]' \
  -s 'attributes.post.logout.redirect.uris=https://app.lorehub.example/*'
```

## Go APIとOIDC issuerの関係

Go APIは `LOREHUB_OIDC_ISSUER` を使ってOIDC discoveryを取得し、トークンのissuer・audience・署名・有効期限を
検証します（既存の `services/api/internal/auth/auth.go` の挙動）。`LOREHUB_OIDC_ISSUER` が空の場合、APIは
現在のbearer-only無効状態を維持します。Keycloakを有効にする場合はissuerをレルムURLに設定します。

ローカル既定のissuerは `http://localhost:8280/realms/lorehub` です。

### ローカルDockerでのdiscovery到達性の注意

`docker compose up` でAPIもコンテナ内で動かす場合、APIコンテナから `http://localhost:8280` へのアクセスは
到達できません（`localhost` はコンテナ自身を指すため）。これはKeycloakのローカル運用でよく知られる制限です。
次のいずれかで運用してください。

1. **APIをホストで起動する**（READMEのホスト開発手順）。ホストからは `http://localhost:8280` に到達できるため、
   discoveryとJWKS取得が成功します。ローカル開発ではこの方法を推奨します。
2. **リバースプロキシを挟む**（下記の本番構成と同じ）。KeycloakとAPIを同じプロキシの背後に置き、issuer URLが
   ブラウザとAPIの両方から到達できるようにします。
3. **内部ホスト名をissuerにする**: `KEYCLOAK_HOSTNAME=http://keycloak:8080` を設定し、
   `LOREHUB_OIDC_ISSUER=http://keycloak:8080/realms/lorehub` に合わせます。APIコンテナからは到達しますが、
   ブラウザと管理コンソールが `keycloak` を解決できないため、ホストの `/etc/hosts` に `127.0.0.1 keycloak` を
   追加する必要があります。

本番ではリバースプロキシ（1番目に相当）が標準構成です。

## ソーシャルIDプロバイダー

各プロバイダーは `LOREHUB_IDP_<PROVIDER>_CLIENT_ID` と `LOREHUB_IDP_<PROVIDER>_CLIENT_SECRET` の両方が
設定されているときだけプロビジョニングされます。どちらも空ならそのプロバイダーはスキップされます。

プロバイダーの資格情報は各開発者コンソールで取得してください。コールバックURLはKeycloak管理コンソールの
当該プロバイダー画面に表示される `Redirect URI` をそのまま使います。形式は次の通りです。

```
http://localhost:8280/realms/lorehub/broker/<alias>/callback
```

本番では `http://localhost:8280` を公開URLに置き換えてください。

### Google

- コンソール: Google Cloud Console > APIs & Services > Credentials > OAuth client ID
- アプリケーション種別: Web application
- 承認済みリダイレクトURI:
  `http://localhost:8280/realms/lorehub/broker/google/callback`
- スコープ: `openid email profile`（Keycloak既定）
- 任意: `LOREHUB_IDP_GOOGLE_HOSTED_DOMAIN` でGoogle Workspaceドメインに制限できます。

### GitHub

- コンソール: GitHub > Settings > Developer settings > OAuth Apps > New OAuth App
- Authorization callback URL:
  `http://localhost:8280/realms/lorehub/broker/github/callback`
- スコープ: `user:email`（Keycloak既定）

### Facebook

- コンソール: Meta for Developers > Apps > 対象アプリ > Facebook Login > Settings
- Valid OAuth Redirect URIs:
  `http://localhost:8280/realms/lorehub/broker/facebook/callback`
- スコープ: `email`（Keycloak既定）

### X（旧Twitter）

Keycloak 26.7では組み込みのTwitterブローカーが非推奨（`twitter-broker` 機能フラグの背後に退避、27.0で削除）
となっており、OAuth 1.0aと未保守のtwitter4jに依存しています。最新のKeycloakとXの公式ドキュメントに従い、
XのOAuth 2.0エンドポイントに対して汎用OAuth v2プロバイダーを設定します。

- コンソール: X Developer Portal > App > User authentication settings > OAuth 2.0
- App type: Web App（Confidential Client）
- Callback URI / Redirect URL:
  `http://localhost:8280/realms/lorehub/broker/x/callback`
- Type of App: Confidential Client（Client Secretが発行されます）
- スコープ: `tweet.read users.read offline.access`
- 使用するエンドポイント:
  - Authorization: `https://twitter.com/i/oauth2/authorize`
  - Token: `https://api.twitter.com/2/oauth2/token`
  - UserInfo: `https://api.twitter.com/2/users/me`
- UserInfo応答は `{"data":{"id","name","username"}}` の形なので、claim名にドット記法
  （`data.id`、`data.username`、`data.name`）を使い、Keycloakのclaimリゾルバでネストを展開します。
- PKCE（S256）を有効化しています。

`bootstrap.sh` はこれらのエンドポイントとclaimマッピングを実際の値で設定します。プレースホルダーではありません。

## アカウント連携のリスク

ソーシャルプロバイダーからのメールは信頼しない設定（`trustEmail=false`）にしています。これは、未検証の
ソーシャルメールで既存アカウントを乗っ取るリスクを減らすためです。初回ログイン時の `firstBrokerLogin` フローで
既存メールとの重複が検出された場合、ユーザーは既存アカウントへのリンクを確認する必要があります。

メールアドレスの重複とアカウントリンクの挙動は、レルム設定の「Identity Provider Mappers」と
「First Broker Login Flow」で調整できます。本番では、リンクを自動承認せず、確認ステップを維持してください。

## SMTPとメール検証・パスワードリセット

レルムはメール検証（`verifyEmail=true`）とパスワードリセット（`resetPasswordAllowed=true`）を有効化しています。
これらはSMTPが設定されていないと機能しません。SMTPは環境ごとに異なるため、レルムJSONには含めず、管理コンソール
またはAdmin CLIで設定します。

```bash
SMTP_JSON='{"host":"smtp.example.com","port":"587","from":"noreply@example.com"'
SMTP_JSON="${SMTP_JSON},\"auth\":\"true\",\"user\":\"smtp-user\",\"password\":\"secret\",\"starttls\":\"true\"}"
docker compose -f infra/compose.yaml exec keycloak \
  /opt/keycloak/bin/kcadm.sh update realms/lorehub \
  -s "smtpServer=${SMTP_JSON}"
```

SMTPの資格情報も `.env` 経由で環境変数に置き、スクリプトから渡すことを推奨します。コンテナのログに
SMTPパスワードが露出しないよう、`kcadm` の引数はログに出力しない運用にしてください。

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
- `KC_HTTP_ENABLED=true` はプロキシがHTTPで転送する場合にのみ有効化し、プロキシ以外からの直接アクセスを
  ネットワークレベルで遮断してください。
- `--proxy-headers=forwarded` を設定済みです。プロキシは `X-Forwarded-Proto`、`X-Forwarded-Host`、
  `X-Forwarded-Port`、`X-Forwarded-For` を正しく送信してください。
- バックチャネル（APIからのdiscovery/JWKS取得）を内部ネットワークで解決するには
  `--hostname-backchannel-dynamic=true` を追加し、プロキシが内部からのリクエストを正しく転送するように
  します。これによりissuerは公開URLのままで、JWKS URIなどのバックチャネルURLが内部到達可能なURLに解決されます。
- `sslRequired` をレルム設定で `external` に変更してください（ローカルJSONは `none` に設定済み）。
- 管理コンソールは `KEYCLOAK_ADMIN_USERNAME` / `KEYCLOAK_ADMIN_PASSWORD` を初回起動時に作成します。
  本番では初回起動後にパスワードを変更し、管理コンソールへのアクセスをネットワークまたはVPNで制限してください。
