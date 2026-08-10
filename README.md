# LoreHub

LoreHubは、Loreリポジトリ向けの共同開発基盤です。LoreをVCSの正本として使い、レビュー、Issue、権限管理、
監査、GitHub Actions互換CIを追加します。Gitリポジトリへ変換したり、Loreのファイル本文をPostgreSQLへ
複製したりしません。

認証とID管理は自己ホストのKeycloakに委譲します。LoreHub本体はユーザーのパスワードを保存せず、Keycloakが
ローカル認証（メール＋パスワード）とGoogle、GitHub、Facebook、Xのソーシャルログインをブローカーします。
詳細は[Keycloak運用ガイド](docs/operations/keycloak.md)と[認証とIDの境界](docs/architecture/identity.md)
を参照してください。

## 現在実装されている範囲

- Lore公式Go SDKを使ったリポジトリ確認とbranch一覧取得
- PostgreSQLのmigration、組織、権限、リポジトリ登録、Issue、レビュー、CI、監査用schema
- OIDCトークンを検証するGo API
- Lore 0.8.6互換のUCS認証、resource限定JWT、JWKS、TLS、protected branch hook
- 組織チーム、直接collaborator、リポジトリ方針、明示的なobliterate権限、Lore Links宣言
- 公開リポジトリ、branch、Issueを表示する英語／日本語UI
- Loreのbranch latestを確認してCIを登録するworker
- `.github/workflows`を`nektos/act`で実行するGitHub Actions互換runner
- PostgreSQL、Lore Server、API、Webを起動する開発用Compose構成

これはGitLab全機能の完成版ではありません。現在のコードは、上記の機能が実際のLoreとPostgreSQLへ接続して動く
最初の製品基盤です。画面に表示するサンプルリポジトリや埋め込みデータはありません。

## 構成

```text
Browser
  └─ Next.js web
       └─ Go API
            ├─ PostgreSQL: users, permissions, issues, reviews, CI, audit
            └─ Lore SDK
                 └─ Lore Server: revisions, branches, files, locks

Go runner
  ├─ polls Lore branch latest
  ├─ clones the requested Lore revision
  └─ runs .github/workflows with act
```

設計判断は[ADR 0001](docs/adr/0001-platform-architecture.md)、責務の分け方は
[コンポーネント境界](docs/architecture/components.md)を参照してください。

## 必要なもの

- Node.js 24 LTS以降
- Go 1.26.5以降
- PostgreSQL 18以降
- Lore Server／CLI 0.8.6
- Lore Go SDK／ネイティブライブラリ0.8.5
- CI runnerを動かす場合は`act` 0.2.89と隔離実行環境

Lore Go SDKの最新公開版は0.8.5で、0.8.6のネイティブライブラリとは互換性がありません。APIだけはSDKと
ネイティブライブラリを0.8.5で揃え、Lore Server／CLIは0.8.6を使います。この組合せで実サーバーへの接続試験を
行っています。

依存関係は、本番利用できる最新の相互互換版へ揃えます。Node.jsは公式方針に従って最新LTSの24系を使います。
Next.js 16.3のlint構成がまだESLint 10とTypeScript 7に対応していないため、lintが成立する最新のESLint 9.39.5と
TypeScript 6.0.3を使用します。

## ローカル実行

Webだけを確認する場合も、埋め込みデータには切り替わりません。APIが停止していることを画面に表示します。
既定のCompose構成はKeycloakを含むため、最初にシークレットを生成します。

```bash
scripts/setup-keycloak-secrets.sh
docker compose -f infra/compose.yaml up --build
```

- Web: <http://localhost:3000>
- API readiness: <http://localhost:8080/health/ready>
- Lore health: <http://localhost:41339/health_check>
- Keycloak管理コンソール: <http://keycloak.localhost:8280/admin/master/console>

Keycloakを使わずAPIの従来挙動を維持する場合は、ホスト上のAPIを `LOREHUB_AUTH_MODE=disabled` で起動します。
ComposeのAPIサービスは既定でKeycloakのhealthcheckを待つため、Keycloakを使わないCompose実行では
`LOREHUB_AUTH_MODE=disabled docker compose -f infra/compose.yaml run --rm --no-deps api` のように
依存サービスを明示的に外してください。

```bash
LOREHUB_AUTH_MODE=disabled go run ./services/api/cmd/lorehub serve
```

Keycloakの構成、ソーシャルプロバイダー、SMTP、本番のTLSとリバースプロキシ、バックアップについては
[Keycloak運用ガイド](docs/operations/keycloak.md)を参照してください。

開発用Lore ServerはデータとローカルCAで署名したTLS証明書をDocker volumeへ保存します。CA秘密鍵は初期化専用の
別volumeに隔離され、Lore/APIからは参照できません。ComposeでもJWT検証とUCS gRPC認証を有効にしています。
ローカルCAのtrust手順、本番の鍵交代、Lore hook、partition境界は
[認可境界の運用ガイド](docs/operations/control-plane-authorization.md)を参照してください。

Lore 0.8.6の`environment.endpoint.auth_url`はUCS認証とRebacが共用します。このComposeは公式の
`ucs-auth://auth.lorehub.localhost:8443`広告を使い、Lore 0.8.6のclientがUCS接続をHTTPSへ変換します。hostのCLIが
同じpublic URLを使えるよう、Lore container内だけAuthURLの名前をローカルbridgeへ解決します。bridgeは設定した内部HTTPS
authorityへCAとSANを検証して転送します。issuer、audience、JWKS、Lore URLも同じmanaged root domainに揃えています。
詳細は運用ガイドの「URL、audience、鍵」を参照してください。

### CI runner

```bash
docker compose -f infra/compose.yaml --profile runner up --build
```

runner profileはホストのDocker socketを渡しません。任意コードを実行するrunnerは、専用のrunner-engineまたはVMを
使い、ジョブごとに隔離してください。本番ではAPIと別の専用ノードに配置し、runner-engineへの接続も専用の認証境界で
保護します。

## ホスト上での開発

```bash
npm ci
npm run dev
```

別のターミナルでAPIを起動します。

```bash
set -a
. ./.env
set +a
export DATABASE_URL=postgresql://lorehub:${POSTGRES_PASSWORD}@localhost:5432/lorehub
export LORE_LIB_PATH=/absolute/path/to/liblore.dylib
go run ./services/api/cmd/lorehub serve
```

認証を使わない開発環境では、`LOREHUB_AUTH_MODE=disabled`（既定値）のまま起動できます。既存のAPIクライアント向け
Bearer認証だけを使う場合は、`LOREHUB_AUTH_MODE=bearer`、`LOREHUB_OIDC_ISSUER`、`LOREHUB_OIDC_AUDIENCE`を
設定します。

ブラウザログインを有効にする場合は、KeycloakをOIDC providerとして`LOREHUB_AUTH_MODE=interactive`を設定し、
`LOREHUB_OIDC_ISSUER`、`LOREHUB_OIDC_AUDIENCE`、`LOREHUB_OIDC_CLIENT_ID`、`LOREHUB_OIDC_CLIENT_SECRET`、
`LOREHUB_OIDC_REDIRECT_URL`、`LOREHUB_PUBLIC_ORIGIN`、32文字以上の`LOREHUB_AUTH_SECRET`を設定します。
`LOREHUB_OIDC_CLIENT_ID`はID tokenの対象（例:`lorehub-web`）、`LOREHUB_OIDC_AUDIENCE`はAPI access tokenの対象
（例:`lorehub-api`）です。両方を同じ値にせず、Keycloakの各tokenに設定したaudienceと一致させます。
Google、GitHub、Facebook、XなどのログインはKeycloak側のbroker設定で追加します。ログイン状態はLoreHubの
サーバー側セッションで管理し、ブラウザCookieへOIDC tokenは保存しません。ログイン開始時は`/auth`専用の短命な
HttpOnly、SameSite=Laxのbinding cookieで開始ブラウザを記録し、callbackでstateと照合します。CookieのSecure属性は
本番で既定有効です。
セッション期限、Cookie名、Path、Domain、Secure属性は`LOREHUB_SESSION_TTL`、
`LOREHUB_SESSION_COOKIE_NAME`、`LOREHUB_SESSION_COOKIE_PATH`、`LOREHUB_SESSION_COOKIE_DOMAIN`、
`LOREHUB_SESSION_COOKIE_SECURE`で変更できます。binding cookie名は`LOREHUB_LOGIN_BINDING_COOKIE_NAME`で変更でき、
ログイン transactionとbinding cookieの期限は`LOREHUB_LOGIN_TRANSACTION_TTL`で変更できます（セッションは最大30日、
transactionは最大15分）。

通常のログインは`GET /auth/login`、Keycloakの登録画面を開始する場合は
`GET /auth/login?prompt=create`を使います。互換性のため`kc_action=register`も受け付けますが、値は厳密に検証し、
認証プロバイダーへは`prompt=create`だけを渡します。その他の`prompt`や`kc_action`は400を返します。

Loreの読み取り・書き込み・mergeとrunnerのcheckoutは、現在の利用者またはrunner専用利用者の短命resource限定
tokenを使います。`LOREHUB_LORE_IDENTITY`は隔離したlocal互換モードでのみ使え、本番では設定できません。

Keycloakを使う場合、ローカルのissuerは
`http://keycloak.localhost:8280/realms/lorehub`、audienceは`lorehub-api`です。本番では公開HTTPSのissuerを設定します。
APIをDockerコンテナで起動する場合のdiscovery到達性については
[Keycloak運用ガイド](docs/operations/keycloak.md)を参照してください。

## APIの主な入口

| Method | Path                                           | 認証          | 目的                   |
| ------ | ---------------------------------------------- | ------------- | ---------------------- |
| `GET`  | `/health/live`                                 | 不要          | プロセスの確認         |
| `GET`  | `/health/ready`                                | 不要          | PostgreSQL接続の確認   |
| `GET`  | `/auth/login`                                  | 不要          | OIDCログイン／登録開始 |
| `GET`  | `/auth/callback`                               | 不要          | OIDCログイン完了       |
| `POST` | `/auth/logout`                                 | CSRF          | セッション終了         |
| `GET`  | `/api/v1/auth/session`                         | 不要          | ログイン状態の確認     |
| `GET`  | `/api/v1/explore/repositories`                 | 不要          | 公開リポジトリ一覧     |
| `POST` | `/api/v1/organizations`                        | OIDC          | 組織作成               |
| `POST` | `/api/v1/organizations/{org}/repositories`     | OIDC          | Loreリポジトリ登録     |
| `GET`  | `/api/v1/repositories/{owner}/{repo}/branches` | OIDC／session | Lore branch一覧        |
| `GET`  | `/api/v1/repositories/{owner}/{repo}/issues`   | 不要          | 公開Issue一覧          |
| `POST` | `/api/v1/repositories/{owner}/{repo}/issues`   | OIDC          | Issue作成              |

更新APIは、既存クライアントからは`Authorization: Bearer <token>`で利用できます。ブラウザセッションで利用する場合は、
`GET /api/v1/auth/session`が返すCSRF tokenを`X-CSRF-Token`ヘッダーに付けます。APIはOIDCのissuer、audience、署名、
有効期限、nonceを検証し、初回ログイン時に外部identityとローカル利用者を関連付けます。

## GitHub Actions互換範囲

runnerは`.github/workflows/*.yml`を`act`へ渡します。`actions/checkout`は、workerが取得済みのLore revisionを使う
処理へ置き換えます。`push`イベントの`before`と`after`にはLore revisionが入ります。

Linuxコンテナで実行できるworkflowを対象にしています。GitHubのAPIそのもの、Git固有コマンド、Windows／macOS
runner、GitHubが管理するrunner imageとの完全一致は提供しません。互換範囲外の機能を黙って成功扱いにはしません。

## 品質確認

```bash
npm run check
```

次をまとめて実行します。

- Prettier、ESLint、TypeScript
- Node.js unit test、Next.js production build
- 1行120文字以下、1ファイル1000行未満の確認
- gofmt、go vet、Go unit test、Go build
- npm audit、govulncheck

CI自体も[GitHub Actions workflow](.github/workflows/ci.yml)で同じ検査を実行します。
