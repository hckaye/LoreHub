# LoreHub

LoreHubは、Loreリポジトリ向けの共同開発基盤です。LoreをVCSの正本として使い、レビュー、Issue、権限管理、
監査、GitHub Actions互換CIを追加します。Gitリポジトリへ変換したり、Loreのファイル本文をPostgreSQLへ
複製したりしません。

## 現在実装されている範囲

- Lore公式Go SDKを使ったリポジトリ確認とbranch一覧取得
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

```bash
cp .env.example .env
docker compose -f infra/compose.yaml up --build postgres lore api web
```

- Web: <http://localhost:3000>
- API readiness: <http://localhost:8080/health/ready>
- Lore health: <http://localhost:41339/health_check>

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
export DATABASE_URL=postgresql://lorehub:lorehub-development@localhost:5432/lorehub
export LORE_LIB_PATH=/absolute/path/to/liblore.dylib
go run ./services/api/cmd/lorehub serve
```

OIDCを有効にする場合は`LOREHUB_OIDC_ISSUER`と`LOREHUB_OIDC_AUDIENCE`を設定します。本番環境では両方が
必須です。Lore側の読み取りidentityは`LOREHUB_LORE_IDENTITY`で指定します。

## APIの主な入口

| Method | Path                                           | 認証 | 目的                 |
| ------ | ---------------------------------------------- | ---- | -------------------- |
| `GET`  | `/health/live`                                 | 不要 | プロセスの確認       |
| `GET`  | `/health/ready`                                | 不要 | PostgreSQL接続の確認 |
| `GET`  | `/api/v1/explore/repositories`                 | 不要 | 公開リポジトリ一覧   |
| `POST` | `/api/v1/organizations`                        | OIDC | 組織作成             |
| `POST` | `/api/v1/organizations/{org}/repositories`     | OIDC | Loreリポジトリ登録   |
| `GET`  | `/api/v1/repositories/{owner}/{repo}/branches` | 不要 | Lore branch一覧      |
| `GET`  | `/api/v1/repositories/{owner}/{repo}/issues`   | 不要 | 公開Issue一覧        |
| `POST` | `/api/v1/repositories/{owner}/{repo}/issues`   | OIDC | Issue作成            |

更新APIは`Authorization: Bearer <token>`を要求します。APIはOIDCのissuer、audience、署名、有効期限を検証し、
初回アクセス時にidentityと利用者を関連付けます。

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
