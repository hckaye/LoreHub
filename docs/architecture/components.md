# コンポーネント境界

## Web

Next.jsは画面表示、ロケール選択、OIDCログイン開始、Go APIへのリクエストを担当する。DBやLore SDKへ直接接続しない。
OIDCのauthorization code交換、PKCE、利用者の紐付け、サーバー側セッション、CSRF tokenはGo APIが担当する。

## API

Go APIは入力検証、認証、権限確認、業務ルール、PostgreSQLのトランザクションを担当する。HTTP handlerから直接SQLや
Lore SDKを呼ばず、機能ごとのサービスを経由する。

## Lore adapter

Loreの公式Go SDKを扱う唯一のパッケージ。次の製品内モデルへ変換する。

- repository: Lore Server上のリポジトリID、URL、既定branch
- branch: branch ID、名前、latest revision、保護とarchiveの状態
- revision: revision ID、番号、親revision、利用者向けのmetadata
- tree entry: パス、種類、サイズ、ファイル固有のmetadata

LoreのIDやrevisionは外部仕様で必要な識別子として保存する。生成物が差し替わっていないことを証明する目的では使わない。

## PostgreSQL

PostgreSQLには共同作業に必要な情報だけを保存する。Loreのファイル本文やrevision treeは保存しない。

主な範囲は、OIDC identity、ログイン transaction、サーバー側セッション、組織、権限、リポジトリ登録、Issue、コメント、
ラベル、マージ要求、レビュー、branch rule、CI run、job、監査記録、outboxである。

## Worker

イベントworkerはLoreの通知を購読し、branch pushなどをPostgreSQLのoutboxへ取り込む。CI workerは
`FOR UPDATE SKIP LOCKED`で実行待ちjobを取得し、Loreから指定revisionをcloneして`act`を実行する。

## オブジェクトストレージ

CIログ、成果物、添付ファイルを保存する。DBには保管先、サイズ、content type、保持期限を保存する。利用者へ返すURLは
短時間だけ有効にし、権限確認後に発行する。
