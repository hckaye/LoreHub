# Webhooks

[English](webhooks.md) | [日本語](webhooks.ja.md)

Repository webhooks send selected events to an external service. Repository administrators can add and manage them
under **Settings > Webhooks**.

## Add a webhook

Enter the following settings:

- **Payload URL:** An HTTPS endpoint with a publicly routable address. Redirects are not followed.
- **Secret:** A value between 16 and 512 bytes. LoreHub does not show it again after saving.
- **Events:** One or more event groups to send.
- **Active:** Clear this option to ignore events while keeping the webhook and its existing history.

The same payload URL can be registered once per repository. Use a separate URL when an integration needs a different
event selection or secret.

## Request format

LoreHub sends an HTTP `POST` request with `Content-Type: application/json` and these headers:

| Header                    | Value                                                   |
| ------------------------- | ------------------------------------------------------- |
| `X-LoreHub-Delivery`      | Unique delivery ID                                      |
| `X-LoreHub-Event`         | Event name, such as `issue.created` or `branch.pushed`  |
| `X-LoreHub-Signature-256` | `sha256=` followed by the HMAC-SHA-256 signature in hex |
| `User-Agent`              | `LoreHub-Hookshot/1.0`                                  |

The JSON body has this shape:

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

The contents of `payload` depend on the event. Use the `event` field to select the expected payload type.

## Verify the signature

Calculate HMAC-SHA-256 over the request body bytes before parsing JSON. Compare the result with
`X-LoreHub-Signature-256` using a constant-time comparison.

```js
import { createHmac, timingSafeEqual } from "node:crypto";

export function validLoreHubSignature(rawBody, header, secret) {
  const expected = `sha256=${createHmac("sha256", secret).update(rawBody).digest("hex")}`;
  const received = Buffer.from(header, "utf8");
  const calculated = Buffer.from(expected, "utf8");
  return received.length === calculated.length && timingSafeEqual(received, calculated);
}
```

Do not parse and re-encode the body before verification. Whitespace and property order are part of the signed bytes.

## Delivery results and retries

Any HTTP status from `200` through `299` completes the delivery. Other responses and connection errors are retried
after 1 minute, 5 minutes, 15 minutes, 1 hour, 6 hours, 12 hours, and 24 hours. The delivery stops after eight failed
attempts.

The settings page shows the response status, attempt count, last update, and recent delivery history. A repository
administrator can redeliver a completed or failed delivery. Manual redelivery keeps the previous attempt history.

LoreHub checks the destination address again for each attempt. Production installations accept HTTPS endpoints on
public addresses only.
