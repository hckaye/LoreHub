# メトリクスとアクセス制限

[English](observability.md) | [日本語](observability.ja.md)

APIは構造化したJSON logを標準出力へ書きます。完了したHTTP requestには、request ID、method、path、対応した
route、status、処理時間が入ります。container logを運用環境のlog収集先へ送り、利用者から報告されたエラーを
調べるときは`X-Request-ID`の値を記録してください。

## Prometheusメトリクス

APIの`GET /metrics`はPrometheus text形式のメトリクスを返します。アクセスには`LOREHUB_METRICS_TOKEN`を
Bearer tokenとして指定します。`scripts/setup-secrets.sh`がこの値を生成します。

```bash
set -a
. ./.env
set +a
curl --fail \
  --header "Authorization: Bearer ${LOREHUB_METRICS_TOKEN}" \
  http://localhost:8080/metrics
```

`/metrics`には監視networkからだけ到達できるようにreverse proxyを設定します。tokenは監視systemの秘密情報
保存先で管理してください。

取得できる値は次のとおりです。

- `lorehub_up`
- `lorehub_http_requests_in_flight`
- method、対応したroute、status別の`lorehub_http_requests_total`
- method、対応したroute、status別の`lorehub_http_request_duration_seconds`
- `lorehub_http_rate_limit_rejections_total`

request pathではなくroute patternを記録するため、リポジトリ名、ユーザー名、object IDごとに別のlabelは
作られません。5xx応答の継続、処理時間の増加、アクセス制限の繰り返し、ready checkの失敗を監視してください。

## Request数を制限する

`/api/`と`/auth/`以下のrequestを、利用者ごとの固定時間枠で制限します。health、metrics、Lore認証情報の取得は
数えません。次の値を設定できます。

- `LOREHUB_RATE_LIMIT_REQUESTS`: 既定値は`600`
- `LOREHUB_RATE_LIMIT_WINDOW`: 既定値は`1m`、設定範囲は`1s`から`1h`

上限を超えたrequestには、`Retry-After`と`rate_limited` error codeを付けてHTTP 429を返します。APIは最大
100,000件の利用者ごとの時間枠を保持し、上限に達している間は新しい利用者からのrequestを拒否します。

通常は、APIへ直接接続した送信元addressを利用者の識別に使います。その送信元networkが
`LOREHUB_RATE_LIMIT_TRUSTED_PROXY_CIDRS`にある場合だけ、`X-Forwarded-For`を右から確認し、信頼済みnetworkの
外側にある最初のaddressを使います。信頼するreverse proxyのnetworkだけをカンマ区切りで指定してください。
proxyは接続元addressを`X-Forwarded-For`へ追加し、API portへの直接接続を許可しない構成にします。

proxy networkの設定例です。

```dotenv
LOREHUB_RATE_LIMIT_TRUSTED_PROXY_CIDRS=172.30.10.0/24
```

利用者が直接APIへ接続する場合や、proxyが`X-Forwarded-For`を正しく管理しない場合は空のままにします。
