# ADR 0003: lh コマンドラインインターフェース

[English](0003-lh-command-line-interface.md) | [日本語](0003-lh-command-line-interface.ja.md)

- Status: Accepted
- Date: 2026-08-13

## Context

LoreHub の機能は `/api/v1` HTTP API として公開されており、ブラウザセッションまたはパーソナルアクセス
トークン（`lhp_` プレフィックス、`Authorization: Bearer` で送信）で認証する。公式クライアントは Web UI
だけである。リポジトリのデータプレーン操作（クローン、同期、プッシュ）は別の `lore` CLI が担い、LoreHub の
パーソナルアクセストークンを使って `lore auth login --token-type api-key` でサインインする。

GitHub の利用者は同じ種類のワークフローを `gh` CLI で動かしている。LoreHub を導入するチームからも同等品が
求められている: Issue やプルリクエストのスクリプト操作、CI 実行の確認、リリース作成、ターミナルからの
生 API アクセスである。

## Decision

`gh` のコマンド体系を LoreHub API に対して踏襲する公式 CLI `lh` を出荷する。

### 配置とツールチェーン

- CLI はこのリポジトリの `cli/` 配下に独立した Go モジュール（`cli/go.mod`）として置き、`lh` という 1 つの
  バイナリを生成する。モジュールを分けることで、サーバー内部パッケージとその依存を CLI から排除し、CLI を
  単独でリリースできるようにする。
- コマンドは `gh` と同じく `spf13/cobra` で組み立てる。構造は `lh <リソース> <動詞>` で、グローバルフラグ、
  コマンドごとのフラグ、フレームワーク生成の `lh help` 出力を持つ。
- CLI は公開の `/api/v1` HTTP API だけを話す。サーバー内部を import しないため、サードパーティ
  クライアントと同じ表面を通ることになる。

### 認証と設定

- `lh auth login` はホストごとにパーソナルアクセストークンを保存する。対話モードはトークンの入力を促し、
  スクリプト用に `--with-token` が標準入力から読む。`lh auth logout` は保存したトークンを削除し、
  `lh auth status` はホスト、認証済みユーザー、トークンの権限と有効期限を表示する。
- API に `GET /api/v1/account` を新設する。パーソナルアクセストークン認証を受け付け、認証済みユーザーと、
  呼び出し元がトークンの場合はそのトークンの id・プレフィックス・権限・有効期限を返すエンドポイントで
  ある。現状これができるエンドポイントはなく（`/api/v1/auth/session` はブラウザ Cookie しか見ず、トークン
  管理エンドポイントはトークンからの呼び出しを拒否する）、これなしには `auth status` と「自分の
  リポジトリ一覧」が実装できない。CLI はログイン検証にこのエンドポイントを使う。
- クレデンシャル保管は OS のクレデンシャルストア（Keychain、Secret Service、Windows Credential Manager）
  を優先し、利用できない場合のみ hosts ファイルへの平文保存にフォールバックし、その旨をログイン時に
  明示する。ホストと秘密でない設定は `~/.config/lh/hosts.yml`（`XDG_CONFIG_HOME` を尊重）に置く。平文
  フォールバックは親ディレクトリを 0700、一時ファイルを最初から 0600 で作って原子的にリネームし、緩んだ
  モードを見つけたら締め直す。
- ホストは HTTPS・小文字 authority・パスと userinfo なしに正規化する。平文 HTTP のホストへトークンを送る
  にはログイン時に明示的な `--insecure-http`（ローカル開発向け）が必要である。トークンをエラー出力、
  デバッグトレース、サブプロセスの argv に出さない。
- ホストとリポジトリの優先順位は具体的なものから順に: `--hostname` / ホスト付きの `--repo
HOST/owner/name`、次に `LH_REPO`、次に保存済みの既定リポジトリ、次に `LH_HOST`、最後に唯一の設定済み
  ホスト。`LH_TOKEN` は明示的に選ばれたホストにだけ適用し、暗黙に導かれたホストには送らない。
- パーソナルアクセストークンでは他のトークンを管理できないため、`lh auth login` がトークンを作ることは
  なく、Web UI のどこで作るかを案内する。将来のデバイス認可フローは、コマンド表面を変えずにトークン貼り
  付けを置き換えられる。

### コマンド体系（初回イテレーション）

| コマンド     | 動詞                                                   |
| ------------ | ------------------------------------------------------ |
| `lh auth`    | `login`, `logout`, `status`                            |
| `lh repo`    | `list`, `view`, `create`, `clone`, `set-default`       |
| `lh issue`   | `list`, `view`, `create`, `comment`, `close`, `reopen` |
| `lh pr`      | `list`, `view`, `create`, `merge`                      |
| `lh release` | `list`, `view`, `create`                               |
| `lh run`     | `list`, `view`, `watch`                                |
| `lh label`   | `list`, `create`, `delete`                             |
| `lh search`  | `repos`, `issues`, `prs`                               |
| `lh api`     | `/api/v1` への生リクエスト                             |

コマンドの補足:

- `lh repo clone` はリポジトリの `lores://` URL を解決し、保存済みトークンで `lore auth login --token-type
api-key` を実行してから `lore` でクローンする。`lore` CLI がない場合は案内付きで失敗する。
- `lh issue list` のフィルターは Web の一覧と同じ（state、author、assignee、label、milestone、検索）。
- 初回イテレーションの `lh pr merge` は範囲を限定する。マージ準備状態を確認し、サーバー側マージを開始し、
  コンフリクトなしで ready 状態に達した場合だけプッシュする。それ以外はブロッカーを表示して非ゼロで終了
  する。Lore には切り離された PR ref がないためローカルチェックアウトは範囲外。
- `lh run watch` は完了までポーリングする。
- `lh api repos/{owner}/{repo} --method POST --field k=v` は生 JSON を出力する。未ラップ機能の逃げ道である。

- リポジトリ文脈: `--repo [HOST/]owner/name` が常に最優先、次に `LH_REPO`、次に `lh repo set-default` で
  保存した既定。Lore 作業コピーからの導出は `lore` CLI が情報を公開してからのフォローアップとする。
  `lh repo clone` はメタデータ用の `read_api` に加えて Lore クレデンシャル交換用の `read_repository`
  （または `write_repository`）が必要で、コマンドは事前に両方を検証し、足りない方を明示する。
- 同じエンドポイントを再利用できる安価な後続動詞（`issue edit`、`pr comment/close/reopen/edit`、`run
cancel/rerun`、`label edit`、`release edit/delete`、`completion`、`config`）は初回イテレーションの直後に
  追加する。
- 出力: TTY では人間向けのテーブル、パイプ時はタブ区切りのプレーン出力、list/view 系には API レスポンスを
  そのまま出す `--json`。エラーは API の problem メッセージを表示して非ゼロで終了する。
- ローカライズ: コマンド出力は `gh` と同じくまず英語のみ。Web の辞書は再利用しない。

### テストとリリース

- ユニットテストはコマンド解析、設定 / ホスト解決、`httptest` サーバーに対する API 操作を対象にする。
- 既存の `api:*` スクリプトに合わせた `npm run cli:*` スクリプト（`gofmt` 検査、`go vet`、`go test`、
  `go build`）を追加し、CI がサーバーと同じゲートで CLI を検査する。
- 配布は `go build` の成果物から始める。パッケージング（Homebrew、インストーラー）はここでは範囲外。

## Rejected alternatives

### lore CLI を拡張する

`lore` CLI は Lore プロジェクトが所有する VCS データプレーンのクライアントである。フォージのワークフロー
（Issue、プルリクエスト、CI）をそこへ入れると、2 つのリリースサイクルと 2 つの所有域が結合する。`gh` と
`git` が分かれているのと同じ理由で分ける。

### Web フロントエンドのコードを共有する Node.js CLI

Node の CLI なら TypeScript の API 型を再利用できるが、Go なら単一の静的バイナリで済むところに、ビルド
エージェントやサーバーへランタイム依存を持ち込むことになる。Go の API クライアントは小さく、安定した API
に対して手書きで維持できる。

### サーバーと同じ Go モジュールにする

`services/api` の内部を import すればプライベート型を無償で使えるが、CLI がサーバーのリリースに溶接され、
依存グラフが肥大化し、非公開の挙動への偶発的な依存が隠れる。CLI は公開 API だけの上に置かなければ
ならない。

## Operational consequences

- 公開 API がスクリプトの互換性表面になる。`/api/v1` の破壊的変更は今後 `lh` の利用者を壊すため、リリース
  ノートで明示する必要がある。
- `lh repo clone` はインストール済みの `lore` CLI に依存する。ない場合、コマンドは実行可能な案内を出して
  失敗する。
- 新しいドキュメント: インストール、ログイン、上記コマンド表を扱う `docs/lh-cli.md`（+ 日本語版）。
