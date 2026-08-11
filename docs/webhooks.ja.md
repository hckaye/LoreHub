# Webhook

[English](webhooks.md) | [日本語](webhooks.ja.md)

リポジトリWebhookは、選択したイベントを外部サービスへ送信します。リポジトリ管理者は
**Settings > Webhooks** から追加と管理ができます。

## Webhookを追加する

次の項目を入力します。

- **送信先URL:** 公開IPアドレスへ接続するHTTPSエンドポイント。リダイレクトには追従しません。
- **シークレット:** 16バイト以上512バイト以下の値。保存後にLoreHubから再表示することはできません。
- **イベント:** 送信するイベントグループを1つ以上選択します。
- **有効:** オフにすると、Webhookと既存の配信履歴を残したままイベントを送信しません。

同じ送信先URLは1つのリポジトリにつき1回だけ登録できます。イベントやシークレットを分ける場合は、別のURLを
使用してください。

## リクエスト形式

LoreHubは `Content-Type: application/json` のHTTP `POST` リクエストを次のヘッダーとともに送信します。

| ヘッダー                  | 値                                                  |
| ------------------------- | --------------------------------------------------- |
| `X-LoreHub-Delivery`      | 配信ごとに異なるID                                  |
| `X-LoreHub-Event`         | `issue.created` や `branch.pushed` などのイベント名 |
| `X-LoreHub-Signature-256` | `sha256=` と16進数のHMAC-SHA-256署名                |
| `User-Agent`              | `LoreHub-Hookshot/1.0`                              |

JSON bodyの形式は次のとおりです。

```json
{
  "deliveryId": "4d4529a3-00b6-40ec-a84d-e6c586e5fb83",
  "event": "issue.created",
  "occurredAt": "2026-08-12T00:00:00Z",
  "repository": {
    "id": "ea254b18-d051-4319-a258-5c875d655691",
    "owner": "acme",
    "name": "game"
  },
  "payload": {}
}
```

`payload` の内容はイベントによって異なります。`event` の値に対応する形式で処理してください。

## 署名を検証する

JSONを解析する前のリクエストbodyを使ってHMAC-SHA-256を計算します。計算結果と
`X-LoreHub-Signature-256` は、処理時間が入力値に左右されない方法で比較してください。

```js
import { createHmac, timingSafeEqual } from "node:crypto";

export function validLoreHubSignature(rawBody, header, secret) {
  const expected = `sha256=${createHmac("sha256", secret).update(rawBody).digest("hex")}`;
  const received = Buffer.from(header, "utf8");
  const calculated = Buffer.from(expected, "utf8");
  return received.length === calculated.length && timingSafeEqual(received, calculated);
}
```

検証前にbodyをJSONへ変換して再出力しないでください。空白とプロパティの順序も署名対象に含まれます。

## 配信結果と再試行

HTTP statusが `200` から `299` までの場合は配信成功です。それ以外のレスポンスと接続エラーは、1分、5分、
15分、1時間、6時間、12時間、24時間の間隔で再試行します。8回失敗すると自動再試行を終了します。

設定画面では、レスポンスのstatus、試行回数、更新日時、最近の配信履歴を確認できます。リポジトリ管理者は、
完了または失敗した配信を手動で再配信できます。手動で再配信しても、それまでの試行履歴は残ります。

LoreHubは配信のたびに送信先アドレスを確認します。本番環境では、公開アドレスのHTTPSエンドポイントだけを
使用できます。
