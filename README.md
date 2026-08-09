# LoreHub

LoreHubは、Loreリポジトリ向けの共同開発基盤です。LoreをVCSの正本として使い、レビュー、Issue、権限管理、
監査、GitHub Actions互換CIを追加します。Gitリポジトリへ変換したり、Loreのファイル本文をPostgreSQLへ
複製したりしません。

認証とID管理は自己ホストのKeycloakに委譲します。LoreHub本体はユーザーのパスワードを保存せず、Keycloakが
ローカル認証（メール＋パスワード）とGoogle、GitHub、Facebook、Xのソーシャルログインをブローカーします。
詳細は[Keycloak運用ガイド](docs/operations/keycloak.md)と[認証とIDの境界](docs/architecture/identity.md)
を参照してください。

## 現在実装されている範囲

- Lore公式Go SDKを使ったリポジトリ確認、branch一覧、revision tree、ファイル、履歴、差分取得
- Loreのbranch merge、競合解決、abort、restart、pushを含むプルリクエストのマージ lifecycle
- PostgreSQLのmigration、組織、権限、リポジトリ登録、Issue、レビュー、CI、監査用schema
- OIDCトークンを検証するGo API
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

開発用Lore Serverはデータと自己署名証明書をDocker volumeへ保存します。認証なしの単一ノード構成なので、公開環境で
使ってはいけません。本番ではLore公式のストレージ、JWT検証、TLS、バックアップ構成を使用してください。

### CI runner

```bash
docker compose -f infra/compose.yaml --profile runner up --build
```

開発用profileはホストのDocker socketをrunnerへ渡します。任意コードを実行できるため、信頼できないリポジトリには
使えません。本番ではAPIと別の専用ノードに配置し、rootlessコンテナまたはVMでジョブごとに隔離してください。

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

LoreHubのコード閲覧とマージ操作は、ブラウザ利用者のLore tokenをLoreへ転送しません。
本番では注入されたcredential issuerを毎回呼び、利用者または明示的なサービス用途、Lore partition、
必要なscopeに結び付いた短命credentialを取得します。`identity`、`token`、`authUrl`、有効期限は
要求と一致しなければ使用せず、AuthURLは設定した`ucs-auth://` authorityと完全一致する必要があります。
利用者のwrite/admin権限、branch rule、レビュー、CI、CSRFはLoreHub APIが先に確認します。
監査記録には実際の利用者を保存します。
本番のcredential issuerが未接続の場合、APIは起動時にfail closedします。
`LOREHUB_LORE_AUTHORITY`にはissuerが返す`ucs-auth://` authorityを設定します。
サービス用principalは用途名だけでは成立せず、対応するJWTの不変なsubjectも必要です。本番では
`LOREHUB_LORE_PUBLIC_READER_SUBJECT`、`LOREHUB_LORE_ACTIONS_RUNNER_SUBJECT`、
`LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT`を明示設定します。subjectが用途名と同じである必要はなく、
issuerが返すcredentialの`identity`は要求したsubjectと完全一致しなければ使用しません。
secret、token、identity設定値をリポジトリへコミットしないでください。
本番でpartitionに対応するcredentialが無い、要求と一致しない、または期限が不正な場合もfail closedになります。
`AuthURL`とtokenはログ、エラー、URLへ出力しません。サービスidentityとsecretは最小権限で管理し、
issuerとsecret managerは運用環境の信頼境界で管理し、短命credentialを毎回発行してください。

ローカル開発とtestだけは、`LOREHUB_LORE_ALLOW_DEVELOPMENT_FALLBACK=true`と
`LOREHUB_LORE_IDENTITY`または開発用`LOREHUB_LORE_CREDENTIALS`を明示した場合に限り、
認証情報のない開発用credentialを使えます。
このcredentialは`InsecureDevelopment`として扱われ、本番用SDKでは拒否されます。
このfallbackはdevelopment環境以外では起動時に拒否されます。
Actions runnerのcredentialもpartitionとread scopeを指定して解決し、ブラウザのコード閲覧・マージ権限とは分離します。

Keycloakを使う場合、ローカルのissuerは
`http://keycloak.localhost:8280/realms/lorehub`、audienceは`lorehub-api`です。本番では公開HTTPSのissuerを設定します。
APIをDockerコンテナで起動する場合のdiscovery到達性については
[Keycloak運用ガイド](docs/operations/keycloak.md)を参照してください。

## APIの主な入口

- `GET`（不要）: `/health/live` — プロセスの確認
- `GET`（不要）: `/health/ready` — PostgreSQL接続の確認
- `GET`（不要）: `/auth/login` — OIDCログイン／登録開始
- `GET`（不要）: `/auth/callback` — OIDCログイン完了
- `POST`（CSRF）: `/auth/logout` — セッション終了
- `GET`（不要）: `/api/v1/auth/session` — ログイン状態の確認
- `GET`（不要）: `/api/v1/explore/repositories` — 公開リポジトリ一覧
- `POST`（OIDC）: `/api/v1/organizations` — 組織作成
- `POST`（OIDC）: `/api/v1/organizations/{org}/repositories` — Loreリポジトリ登録
- `GET`（不要）: `/api/v1/repositories/{owner}/{repo}/branches` — Lore branch一覧
- `GET`（任意認証）: `/api/v1/repositories/{owner}/{repo}/tree`、`/file`、`/file/history`、`/raw`
  — Loreのツリーとファイル
- `GET`（任意認証）: `/api/v1/repositories/{owner}/{repo}/revisions`、`/diff` — 履歴と差分
- `GET`（不要）: `/api/v1/repositories/{owner}/{repo}/issues` — 公開Issue一覧
- `POST`（OIDC）: `/api/v1/repositories/{owner}/{repo}/issues` — Issue作成
- `GET`（任意認証）: `/api/v1/repositories/{owner}/{repo}/merge-requests/{number}/merge-readiness`
  — マージ条件確認
- `POST`（CSRF／write）: `/api/v1/repositories/{owner}/{repo}/merge-requests/{number}/merge/start`
  と `/merge/continue` — Lore merge開始／再開
- `POST`（CSRF／write）: `/api/v1/repositories/{owner}/{repo}/merge-requests/{number}/merge`
  — Lore pushとDB確定

更新APIは、既存クライアントからは`Authorization: Bearer <token>`で利用できます。ブラウザセッションで利用する場合は、
`GET /api/v1/auth/session`が返すCSRF tokenを`X-CSRF-Token`ヘッダーに付けます。APIはOIDCのissuer、audience、
署名、有効期限、nonceを検証し、初回ログイン時に外部identityとローカル利用者を関連付けます。

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
