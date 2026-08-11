# コンポーネントの役割

[English](components.md) | [日本語](components.ja.md)

## Webアプリケーション

Next.jsは画面表示、言語選択、OIDCログイン・登録の開始、Go APIへのリクエストを担当します。PostgreSQLや
Lore SDKへ直接接続しません。authorization codeの交換、ID tokenとaccess tokenのaudience検証、PKCE、
ログインを開始したブラウザの確認、ユーザーの紐付け、サーバー側session、CSRF tokenはGo APIが処理します。

## API

Go APIは入力検証、認証、権限確認、業務ルール、PostgreSQLのtransactionを担当します。HTTP handlerは機能ごとの
serviceを呼び出し、SQLやLore SDKを直接実行しません。

## Lore adapter

Lore adapterは、Lore公式Go SDKを呼び出す唯一のpackageです。Loreの情報を次のアプリケーションmodelへ変換します。

- リポジトリ: Lore ServerのリポジトリID、URL、既定branch
- branch: branch ID、名前、最新revision、保護状態、archive状態
- revision: revision ID、番号、親revision、画面表示用metadata
- tree entry: path、種類、size、file metadata

保存したLoreの識別子は、リポジトリとrevisionの取得に使用します。

## PostgreSQL

PostgreSQLには、OIDC identity、ログインtransaction、サーバー側session、組織、権限、リポジトリ登録、Issue、
comment、label、pull request、review、branch rule、CI run、job、監査記録、outbox eventを保存します。Loreの
ファイル本文とrevision treeはLoreで管理します。

## Worker

event workerはLoreの通知を購読し、branch pushなどのeventをPostgreSQLのoutboxへ記録します。CI workerは
`FOR UPDATE SKIP LOCKED`で実行待ちjobを取得し、指定されたLore revisionをcloneして`act`を実行します。

## オブジェクトストレージ

CI log、artifact、添付ファイルを保存します。PostgreSQLには保管先、size、content type、保持期限を保存します。
download URLは短時間で失効し、アクセス権を確認してから発行します。
