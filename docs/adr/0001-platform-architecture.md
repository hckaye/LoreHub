# ADR 0001: LoreHubの基本構成

- 状態: 採用
- 日付: 2026-08-09

## 背景

LoreHubはLoreのリポジトリを中心に、コード閲覧、Issue、レビュー、権限管理、監査、CIを提供する。
VCSをGitへ置き換えたり、Loreのファイルやリビジョンを別のDBへ複製したりしてはならない。

Loreは1.0未満で、APIや保存形式が更新される可能性がある。一方、LoreはC APIと公式Go SDKを公開している。
Giteaは長期運用されているGo製の開発基盤であり、機能ごとのサービス分割や権限確認の置き場所を考えるうえで
参考になる。ただし、Git固有の概念や処理は持ち込まない。

## 決定

### 実行単位

- `apps/web`に相当する画面はNext.jsで実装する。現在はリポジトリ直下の`src/app`に置く。
- API、権限確認、PostgreSQLの更新、非同期処理、CIの制御はGoで実装する。
- Lore Serverがリビジョン、ブランチ、ファイル、ロックの正本になる。
- PostgreSQLがユーザー、組織、権限、Issue、レビュー、CI、監査記録の正本になる。
- CIジョブのログと成果物はオブジェクトストレージに置き、PostgreSQLには参照先と状態だけを保存する。

最初は一つのGoバイナリに`serve`、`migrate`、`runner`を持たせる。デプロイ時は同じバイナリを別プロセスとして
実行する。コードは機能ごとのパッケージに分け、負荷や障害範囲が広がったときに別サービスへ分離できるようにする。

### Loreとの接続

Goバックエンドは公式の
[Lore Go SDK](https://github.com/EpicGames/lore-go)だけを通してLoreを操作する。
SDK固有の型は`internal/lore`から外へ出さない。画面やIssue機能はLoreのライブラリ更新に依存しない。

一覧取得などの読み取りはbare cloneを共有キャッシュへ作り、必要なリビジョンツリーだけを取得する。CIは指定された
リビジョンを一時ディレクトリへcloneし、その内容を実行対象にする。PostgreSQLへファイル本文は保存しない。

LoreHubのブラウザ認証とLoreのcredentialは分離する。利用者の権限とCSRFをAPIで確認した後、Loreのread/write操作は
repository partition、呼び出し元の利用者または明示的なサービス用途、scopeを組にして解決する。本番credentialは
identity、短命token、正確なAuthURLをsecret managerから実行時に注入する。共通identityを利用者の認証確認なしに使ってはならない。
認証値が欠けた場合はfail closedとし、認証情報のないcredentialは明示的なdevelopment/test fixtureに限定する。
LoreHubのログインはOIDC authorization codeとPKCEを使い、APIはアクセストークンの発行者と対象を検証する。ブラウザは
OIDC tokenを保持せず、Go APIが期限付きサーバー側セッションをCookieで管理する。既存のBearer APIクライアントも維持する。

### データ更新

複数テーブルを更新する処理はPostgreSQLのトランザクション内で実行する。外部通知は同じトランザクションで
outboxへ記録し、workerが再送する。重複通知は外部イベントIDまたは業務上の一意制約で止める。

マージ要求には作成時と最終確認時の対象ブランチのlatest revisionを保存する。マージ直前にLoreへ再確認し、値が
変わっていたらレビューとCIを再評価する。Loreのbranch merge操作が成功する前にマージ済みへ更新しない。

### GitHub Actions互換CI

リポジトリ内の`.github/workflows/*.yml`を入力とし、実行エンジンには
[nektos/act](https://github.com/nektos/act)を使う。LoreHubは次だけを担当する。

1. Loreのbranch push通知を受け、同じイベントを一度だけキューへ登録する。
2. 指定されたLore revisionを隔離された作業場所へcloneする。
3. GitHub Actions互換のイベントJSON、環境変数、短命な権限トークンを作る。
4. `act`を非特権の隔離環境で実行し、状態、ログ、成果物を保存する。

`act`はGitHubのサービスそのものではないため、Windows/macOS runner、GitHub固有API、未対応構文には差がある。
対応範囲をAPIと画面で明示し、GitHub公開のワークフロー例を使った実行テストで差を検出する。

任意コードを実行するrunnerはAPIと同じホストへ置かない。本番では信頼ドメインごとに専用・短命なrunner基盤を
使い、ホストのDocker socketを直接渡さない。Composeのprofileは`docker:29.4.0-dind-rootless`へmTLS（2376）で
接続する。runner-data（runner／PostgreSQL／Lore／API）とrunner-control（runner／engine）をinternal network
として分離し、engineはrunner-dataへ接続しない。runner-egressもinternalとし、engineとforward proxyだけを
接続する。proxyだけがuplinkを持ち、runner-action経由のact/action downloadもproxyを通る。API／Webは
runner-controlへ接続しない。job containerは専用internal networkと使い捨てproxy gatewayを使い、外向きの
直接経路を持たない。

engine境界の権限とjob containerのCPU、メモリ、PID、capability、namespace制限を分けて管理する。Docker
Desktopでcgroupが使えない場合、Composeの上限はouter engine全体の上限であり、jobごとのsecurity limitではない。
本番の各trust domainにはAPIから分離した専用・使い捨てrunner node／podと、gVisor、Kata等の検証済み隔離層を
必須とする。Lore読み取りはservice subject、repository partition、read scopeを受ける短命credential issuerを
必須とし、productionで共有identityやファイルcredentialへfallbackしない。開発環境だけ明示的なfallbackを許可する。

### 画面と翻訳

URLの先頭にロケールを置く。表示文は辞書から取得し、コンポーネントへ日本語を直接埋め込まない。画面はGiteaや
GitHubの情報配置を参考にするが、用語はLoreの`revision`、`branch latest`、`working tree`に合わせる。

コンポーネントは役割ごとに分割する。1行120文字以下をlintで必須にし、1ファイル1000行未満を上限にする。

## 採用しなかった案

### Giteaをバックエンドとして使う

GiteaはGitを正本にするため採用しない。UI、権限、Issue、レビューの考え方だけを参考にする。

### Next.jsだけでLoreへ接続する

ネイティブライブラリの配置、長時間処理、接続キャッシュ、CI workerをWebプロセスへ混ぜることになるため採用しない。

### PostgreSQLへLoreの内容を保存する

Loreの大容量ファイル、重複排除、部分取得を損ない、正本が二つになるため採用しない。

## 運用上の結果

Lore Go SDKと対応するネイティブライブラリを同じリリース番号で配布する必要がある。Lore更新は専用パッケージの
統合テストを通してから行う。Lore Serverの本番用ストレージはLore公式の構成に従い、LoreHub用PostgreSQLとは
分離してバックアップと復旧を行う。
