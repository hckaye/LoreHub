# lh CLI

[English](lh-cli.md) | [日本語](lh-cli.ja.md)

`lh`はLoreHubのコマンドラインクライアントです。Forgeの操作にはLoreHub APIを使います。クローン、同期、pushなどの
リポジトリのデータ操作には`lore` CLIを使います。

## インストール

Go 1.26以降が必要です。リポジトリのルートでバイナリをビルドします。

```bash
mkdir -p bin
(cd cli && go build -o ../bin/lh ./cmd/lh)
```

`bin`を`PATH`に追加するか、バイナリのパスを指定して実行します。

## ログイン

**アカウント設定**の**Personal access tokens**でPersonal access tokenを作成します。読み取り専用のAPIコマンドには
`read_api`を使い、LoreHubのデータを変更するコマンドには`api`を使います。`lh repo clone`には`read_repository`または
`write_repository`も必要です。利用できる権限は[Personal access token](personal-access-tokens.ja.md)で確認できます。

対話モードでログインすると、入力したtokenは表示されません。

```bash
lh --host lorehub.example auth login
```

スクリプトでは、標準入力からtokenを渡します。

```bash
lh --host lorehub.example auth login --with-token < token.txt
```

保存済みのホスト、ユーザー、権限、有効期限を確認します。

```bash
lh --host lorehub.example auth status
```

`--host`を指定しない場合は`LH_HOST`でホストを選びます。`LH_TOKEN`を設定すると、選択したホストのtokenをhosts
ファイルへ書き込まずに使えます。設定ファイルの既定値は`~/.config/lh/hosts.yml`です。`XDG_CONFIG_HOME`を設定している
場合は`$XDG_CONFIG_HOME/lh/hosts.yml`を使います。

## リポジトリの指定

リポジトリを操作するコマンドは次の順に対象を選びます。

1. `--repo [HOST/]OWNER/NAME`
2. `LH_REPO`
3. `lh repo set-default`で保存した既定のリポジトリ

選択したホストに既定のリポジトリを保存します。

```bash
lh --host lorehub.example repo set-default acme/widget
```

## コマンド

| コマンド                                                        | 説明                          |
| --------------------------------------------------------------- | ----------------------------- |
| `lh auth login`, `logout`, `status`                             | ホストごとの API トークン管理 |
| `lh repo list`, `view`, `create`, `clone`                       | リポジトリの一覧と管理        |
| `lh issue list`, `view`, `create`, `comment`, `close`, `reopen` | Issue の管理                  |
| `lh pr list`, `view`, `create`, `merge`                         | プルリクエストの管理          |
| `lh release list`, `view TAG-or-ID`, `create`                   | リリースの管理                |
| `lh run list`, `view NUMBER`, `watch NUMBER`                    | Actions 実行の確認            |
| `lh label list`, `create`, `delete NAME`                        | ラベルの管理                  |
| `lh search repos QUERY`, `issues QUERY`, `prs QUERY`            | ホスト内の検索                |
| `lh api PATH`                                                   | `/api/v1` への生リクエスト    |

コマンドの補足:

- `lh repo clone` はリポジトリ URL で `lore` CLI にログインしてから `lore clone` を実行します。
- `lh release create` は `--tag`、`--title`、`--notes`、`--branch` を受け付けます。
- `lh run watch` は `--interval` と `--timeout` を受け付け、結論が `success` の場合だけ正常終了します。
- `lh label create` は `--name`、`--color`、`--description` を受け付けます。`delete` は名前をラベル ID に
  解決します。

`lh repo clone`には`PATH`上の`lore`バイナリが必要です。実行前にAPI tokenの権限を確認します。必要な権限がない場合は
不足している権限を表示し、`lore`を実行しません。

## JSON出力

list、view、searchのコマンドにグローバルフラグ`--json`を付けると、APIレスポンスをインデント付きJSONで出力します。
フラグはコマンドの前後どちらにも置けます。

```bash
lh --repo acme/widget --json issue list
lh --repo acme/widget release view v1.0.0 --json
lh --repo acme/widget --json run view 42
```

`--json`を付けない場合、端末には表を表示します。パイプへ出力すると、タブ区切りの行になります。

## 使用例

リリースを作成して確認します。

```bash
lh --repo acme/widget release create \
  --tag v1.0.0 \
  --title "First release" \
  --notes "Initial public release" \
  --branch main
lh --repo acme/widget release view v1.0.0
```

workflow runの終了を待ち、スクリプトで終了コードを使います。

```bash
lh --repo acme/widget run watch 42 --interval 5s --timeout 30m
```

labelを作成し、名前で削除します。

```bash
lh --repo acme/widget label create --name bug --color ff0000 --description "Confirmed defect"
lh --repo acme/widget label delete bug
```

JSONツールで検索結果を処理します。

```bash
lh --json search prs "release notes" | jq '.pullRequests[] | [.repository.slug, .number, .title]'
```
