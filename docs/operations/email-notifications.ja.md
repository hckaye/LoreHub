# 通知メール

[English](email-notifications.md) | [日本語](email-notifications.ja.md)

利用者は**設定**の**通知設定**でメール通知を有効にできます。チームとリポジトリのイベント設定は、通知一覧と
メール通知の両方に適用されます。メール通知を無効にすると、送信開始前のメールは取り消されます。

## ローカルでメールを確認する

Docker Composeは通知メールをMailpitへ送信します。サービスを起動し、<http://localhost:8025>で受信した
メールを確認できます。

```bash
scripts/setup-secrets.sh
docker compose -f infra/compose.yaml up --build
```

Mailpitはローカルテスト用です。Webポートは`127.0.0.1`だけに公開されます。ポート`8025`を使用中の場合は
`MAILPIT_HOST_PORT`を変更してください。

## 本番SMTPを設定する

APIを起動する前に次の値を設定します。

| 環境変数                                    | 値                                   |
| ------------------------------------------- | ------------------------------------ |
| `LOREHUB_NOTIFICATION_EMAIL_ENABLED`        | `true`                               |
| `LOREHUB_SMTP_HOST`                         | SMTPサーバーのホスト名               |
| `LOREHUB_SMTP_PORT`                         | SMTPサーバーのポート                 |
| `LOREHUB_SMTP_FROM_ADDRESS`                 | 送信元メールアドレス                 |
| `LOREHUB_SMTP_FROM_NAME`                    | 送信者名                             |
| `LOREHUB_SMTP_TLS_MODE`                     | 明示TLSは`starttls`、暗黙TLSは`tls`  |
| `LOREHUB_SMTP_USERNAME`                     | 認証を使う場合のSMTPユーザー名       |
| `LOREHUB_SMTP_PASSWORD`                     | 認証を使う場合のSMTPパスワード       |
| `LOREHUB_NOTIFICATION_EMAIL_POLL_PERIOD`    | 配信確認の間隔。既定値は`2s`         |
| `LOREHUB_NOTIFICATION_EMAIL_SEND_TIMEOUT`   | 1通の送信タイムアウト。既定値は`10s` |
| `LOREHUB_NOTIFICATION_EMAIL_LEASE_DURATION` | 配信処理の占有時間。既定値は`30s`    |
| `LOREHUB_NOTIFICATION_EMAIL_MAX_ATTEMPTS`   | 自動試行回数の上限。既定値は`8`      |

設定が不足している場合、APIは起動しません。本番では`LOREHUB_SMTP_TLS_MODE=none`も拒否します。SMTP認証を
使う場合はユーザー名とパスワードを両方設定し、認証が不要な場合は両方とも空にします。

メール内のリンクには`LOREHUB_PUBLIC_ORIGIN`を使います。本番では公開HTTPS URLを設定してください。

## 送信失敗を再試行する

送信に失敗すると30秒後に再試行し、待機時間を最大1時間まで倍増します。処理中のAPIが停止した場合は、占有時間の
終了後に別のAPIが送信を再開できます。設定した試行回数に達すると送信を終了します。APIログには配信IDと試行回数を
記録しますが、メール本文とSMTPパスワードは記録しません。

アカウント確認とパスワード再設定のメールにはKeycloakのSMTP設定を使います。設定方法は
[Keycloak運用ガイド](keycloak.ja.md#メール検証パスワードリセットsmtp)を参照してください。
