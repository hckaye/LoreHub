# リソース制限

[English](resource-limits.md) | [日本語](resource-limits.ja.md)

APIとLore Serverは、ユーザーあたりの組織数、組織あたりのリポジトリ数、pushするリビジョンの
ファイルツリーサイズ、リポジトリごとの累計アップロード量を制限できます。

## ユーザーあたりの組織数

`api`サービスに`LOREHUB_MAX_ORGANIZATIONS_PER_USER`を設定します。既定値は`0`で、上限なしです。
上限を超える組織作成はHTTP 409を返し、problem codeは`organization_limit`です。Web UIには
その内容が表示されます。

## 組織あたりのリポジトリ数

`api`サービスに`LOREHUB_MAX_REPOSITORIES_PER_ORGANIZATION`を設定します。既定値は`0`で、上限なしです。
ホストされたリポジトリの新規作成と、既存リポジトリの取り込みの両方に適用されます。
すでに登録したリポジトリの準備が失敗したあと、同じリポジトリを再試行しても、この上限では拒否されません。
上限を超えるとHTTP 409を返し、problem codeは`repository_limit`です。

## push時のリポジトリサイズ

`api`サービスに`LOREHUB_MAX_REPOSITORY_SIZE_BYTES`を設定します。既定値は`0`で、上限なしです。
Lore Serverは、pushされた各リビジョンのファイルツリー合計サイズをAPIへ報告します。
上限を超えるpushは、ブランチが更新される前に拒否されます。公式の`lore`クライアントは、
サーバーのメッセージを表示します。拒否されたpushで既に送られたデータは、管理者がobliterateするまで
Loreのストアに残ります。

## インスタンス管理画面での上書き

3つのAPI制限は、インスタンス管理の設定画面からも上書きできます。データベースの上書きが
環境変数より優先されます。上書きを消すと、環境変数の既定値に戻ります。

## Lore Serverのアップロード上限

ビルド時のパッチが、`infra/lore/production.toml`のLore Server設定に`[upload_quota]`を追加します。

- `enabled`
- `default_bytes`: リポジトリごとに受信を許す累計バイト数。書き込み前に計上します。
  上限を超えるアップロードはOversizedエラーで拒否されます。
- `state_path`: リポジトリごとのカウンタを`/data`配下に保存します
- `flush_seconds`

カウンタは、現在のディスク使用量ではなく、累計の受信バイト数です。履歴の書き換えと拒否された
pushも消費します。`default_bytes`は32 MiBです。典型的な10 MBのリポジトリサイズ上限より大きく、
履歴用の余裕を残します。

## 公開テスト環境の推奨設定

```dotenv
LOREHUB_MAX_ORGANIZATIONS_PER_USER=1
LOREHUB_MAX_REPOSITORIES_PER_ORGANIZATION=1
LOREHUB_MAX_REPOSITORY_SIZE_BYTES=10485760
```

Lore Serverの`[upload_quota]`は`infra/lore/production.toml`の既定値のまま使います。
`default_bytes`は32 MiBです。最後の安全策として、Loreのデータボリューム自体にも容量上限を
設定してください。
