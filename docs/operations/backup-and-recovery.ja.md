# バックアップと復元

[English](backup-and-recovery.md) | [日本語](backup-and-recovery.ja.md)

バックアップコマンドは、保存データを書き換えるサービスを停止し、PostgreSQLの論理バックアップと必要な
Docker volumeのarchiveを作成します。完了後は、バックアップ前に動いていたサービスを再開します。
PostgreSQLはバックアップ中も動作します。

## バックアップを作成する

運用中のソースがあるリポジトリで実行します。

```bash
scripts/backup.sh --output /srv/backups/lorehub-2026-08-14
```

保存先は、まだ存在しないdirectoryを指定します。`.env`以外の設定ファイルや`lorehub`以外のCompose project名を
使う場合は、`--env-file`と`--project-name`を指定します。

バックアップには次のデータが入ります。

- LoreHub用PostgreSQLの論理バックアップ。任意のKeycloakプロファイルが起動している場合はKeycloak用も含む
- Loreリポジトリの保存領域
- APIの保存データ、署名鍵、TLS鍵、CAの状態
- runnerのリポジトリcache、log、artifact
- Keycloakの補助データ（任意のKeycloakプロファイル起動時のみ）
- 秘密値を含む環境設定ファイルのコピー
- `manifest.txt`に記録した作成日時、Compose project名、source commit

Mailpitのメッセージ、Docker build layer、runner container layer、PostgreSQLのvolumeそのものは含みません。
PostgreSQLはvolumeのコピーではなく論理バックアップから復元します。

バックアップには認証情報と秘密鍵が含まれます。サービス運用者だけが読める場所に保存し、Docker hostとは別の
storageにもコピーしてください。同じhostだけに保存したバックアップは、hostの障害時に利用できません。

## バックアップを復元する

`manifest.txt`の`source_commit`と同じLoreHubのソースを使います。別のdeploymentから取得したバックアップや、
新しいversionとの組み合わせは、先に隔離したhostで確認してください。

復元コマンドは指定したCompose projectのデータを置き換えます。`--confirm-project`には対象のproject名と完全に
同じ値を指定します。

```bash
scripts/restore.sh \
  --backup /srv/backups/lorehub-2026-08-14 \
  --project-name lorehub \
  --confirm-project lorehub
```

既定では現在の`.env`を使います。新しいhostでは、保存済みの環境設定ファイルを明示して復元します。

```bash
scripts/restore.sh \
  --backup /srv/backups/lorehub-2026-08-14 \
  --project-name lorehub \
  --confirm-project lorehub \
  --restore-environment
```

`--restore-environment`は対象の環境設定ファイルを保存済みのコピーで置き換えます。別の場所に保存する場合は
`--env-file PATH`を指定します。

コマンドはCompose projectを停止し、保存対象のvolumeを置き換えます。2つのdatabaseを作り直してから論理
バックアップを読み込みます。完了時に動いているのは、2つのdatabase serviceだけです。

## サービスを起動する前に確認する

APIを公開する前に、復元したデータを確認します。

1. LoreHub用databaseを照会し、組織、リポジトリ、ユーザー、リポジトリ作成状態が想定どおりか確認します。
2. 復元したLoreの保存領域をLoreで開き、`repositories.lore_repository_id`の各値が想定したリポジトリを指すか
   確認します。
3. 最近の監査記録と送信待ち記録を調べ、リポジトリの作成、削除、移行が途中で残っていないか確認します。
4. サービスを起動し、`/health/ready`、ログイン、リポジトリ閲覧、Loreの読み取り操作を確認します。

確認後に基本サービスを起動します。

```bash
docker compose -f infra/compose.yaml up --detach --wait
```

同梱runnerを使う場合は`--profile runner`を追加します。復元コマンドの`--start`と`--runner`は、確認処理を
test側で実行する自動復元test向けです。

## 定期的に復元を試す

最新のバックアップを別hostの隔離したCompose projectへ復元します。ログイン、リポジトリID、最近のIssueと
pull request、release情報、Loreの読み取りを確認します。バックアップの作成日時と、復元にかかった時間を記録します。
PostgreSQL、Keycloak、Lore、永続volumeの構成を変更した場合も実行してください。
