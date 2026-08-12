# LoreHub

[English](README.md) | [日本語](README.ja.md)

LoreHubは、Loreを使うチーム向けのセルフホスト型共同開発サービスです。Loreリポジトリの閲覧、Issue、
pull request、review、release、GitHub Actions互換CIを一つのWebアプリケーションで利用できます。操作の流れと
画面構成はGitHubとGitLabを参考にし、version controlにはLoreを使用します。

## Dockerで起動する

Docker EngineまたはDocker DesktopとDocker Composeをインストールし、次を実行します。

```bash
scripts/setup-keycloak-secrets.sh
docker compose -f infra/compose.yaml up --build
```

<http://localhost:3000>を開きます。初回起動時はデータがありません。**Sign up**からアカウントを作成し、
組織とLoreリポジトリを作成してください。

ローカルサービスのURLは次のとおりです。

- Web: <http://localhost:3000>
- API health: <http://localhost:8080/health/ready>
- Lore Server health: <http://localhost:41339/health_check>
- Keycloak管理画面: <http://keycloak.localhost:8280/admin/master/console>
- ローカルメール受信画面: <http://localhost:8025>

データを残したまま停止するには、次を実行します。

```bash
docker compose -f infra/compose.yaml down
```

## 主な機能

- ファイル、branch、revision、履歴、差分の閲覧
- Loreの[ファイルロック](docs/file-locks.ja.md)によるバイナリファイルの競合防止
- Issue、label、comment、projectによるタスク管理
- バージョン履歴付きの[Wiki](docs/wiki.ja.md)によるリポジトリ文書の管理
- [pull request](docs/pull-request-reviews.ja.md)、review、branch ruleによる変更管理
- Lore branchのmerge、競合解決、結果のpush
- releaseの公開と外部配布ファイルへのリンク
- 組織、team、リポジトリの公開範囲とアクセス権限の管理
- Star、Watch、[アプリ内通知とメール通知](docs/operations/email-notifications.ja.md)、検索
- Email/Passwordまたは設定済みのソーシャルプロバイダーによるログイン
- [Personal access token](docs/personal-access-tokens.ja.md)によるLore CLIとAPI clientの認証
- 英語と日本語の画面
- GitHub Actions互換workflow、log、artifact、variables、secrets、SARIF結果
- [Webhook](docs/webhooks.ja.md)による外部サービスへのイベント通知と配信履歴

LoreHubは開発中です。GitHubとGitLabにあるすべての画面やAPIにはまだ対応していません。

## ログイン方法を追加する

Email/Passwordはローカルの初期設定で使えます。Google、GitHub、Facebook、Xは、各サービスのclient IDと
client secretを`.env`に追加するとログイン画面に表示されます。設定手順とcallback URLは
[Keycloak運用ガイド](docs/operations/keycloak.ja.md)を参照してください。

## GitHub Actions互換CIを起動する

runner profileを指定して起動します。

```bash
docker compose -f infra/compose.yaml --profile runner up --build
```

runnerは`.github/workflows/*.yml`と`.yaml`を実行します。`actions/checkout@v4`は、指定されたLore revisionを
jobのworkspaceへ配置します。

Linux containerのworkflowに対応しています。WindowsとmacOSのrunner、GitHub管理runner imageとの完全な一致には
対応していません。対応するworkflow構文と本番の構成は
[Actions runnerガイド](docs/runner-actions.ja.md)を参照してください。

## ローカルで開発する

Dockerを使わずに起動する場合は、次のバージョンが必要です。

- Node.js 24.19以降
- Go 1.26.5以降
- PostgreSQL 18以降
- Lore ServerとCLI 0.8.6
- Lore Go SDKとネイティブライブラリ0.8.5

Webを起動します。

```bash
npm ci
npm run dev
```

別のターミナルでAPIを起動します。`LORE_LIB_PATH`には、使用するOS向けの`liblore`を指定してください。

```bash
set -a
. ./.env
set +a
export DATABASE_URL="postgresql://lorehub:${POSTGRES_PASSWORD}@localhost:5432/lorehub"
export LORE_LIB_PATH=/absolute/path/to/liblore
cd services/api
go run ./cmd/lorehub migrate
go run ./cmd/lorehub serve
```

ログインなしでローカル開発するときだけ`LOREHUB_AUTH_MODE=disabled`を設定できます。本番では認証を
無効にしないでください。

## 変更を確認する

```bash
npm run check
```

format、ファイル長制限、lint、型検査、test、production build、依存関係の脆弱性を確認します。
[GitHub Actions](.github/workflows/ci.yml)でも同じ検査を実行します。

## 設計と運用

- [設計判断](docs/adr/0001-platform-architecture.ja.md)
- [Web、API、Lore Serverの責務](docs/architecture/components.ja.md)
- [ログインとIDの管理](docs/architecture/identity.ja.md)
- [Frontendの認証](docs/frontend-auth.ja.md)
- [リポジトリのアクセス権限](docs/operations/control-plane-authorization.ja.md)
- [ファイルロック](docs/file-locks.ja.md)
- [Keycloak、ソーシャルログイン、メール、バックアップ](docs/operations/keycloak.ja.md)
- [通知メール](docs/operations/email-notifications.ja.md)
- [GitHub Actions互換範囲とrunner運用](docs/runner-actions.ja.md)

## ライセンス

LoreHubは[MIT License](LICENSE)で提供します。
