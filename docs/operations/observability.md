# Metrics and rate limiting

[English](observability.md) | [日本語](observability.ja.md)

The API writes structured JSON logs to standard output. Each completed HTTP request includes its request ID, method,
path, matched route, status, and duration. Send container logs to the deployment's log collector and retain the
`X-Request-ID` value when investigating an error reported by a client.

## Prometheus metrics

The API exposes Prometheus text metrics at `GET /metrics`. The endpoint requires the bearer token in
`LOREHUB_METRICS_TOKEN`. `scripts/setup-secrets.sh` generates this value.

```bash
set -a
. ./.env
set +a
curl --fail \
  --header "Authorization: Bearer ${LOREHUB_METRICS_TOKEN}" \
  http://localhost:8080/metrics
```

Configure the reverse proxy so that `/metrics` is reachable only from the monitoring network. Store the token in the
monitoring system's secret store.

The endpoint reports:

- `lorehub_up`
- `lorehub_http_requests_in_flight`
- `lorehub_http_requests_total` by method, matched route, and status
- `lorehub_http_request_duration_seconds` by method, matched route, and status
- `lorehub_http_rate_limit_rejections_total`

Matched route patterns are used instead of request paths, so repository names, user names, and object IDs do not create
new metric labels. Alert on sustained 5xx responses, high request duration, repeated rate-limit rejections, and failed
readiness checks.

## Request rate limit

Requests under `/api/` and `/auth/` use a fixed per-client window. Health, metrics, and Lore authentication discovery
endpoints are not counted. Configure the limit with:

- `LOREHUB_RATE_LIMIT_REQUESTS`, default `600`
- `LOREHUB_RATE_LIMIT_WINDOW`, default `1m`, allowed range `1s` to `1h`

An excess request receives HTTP 429 with `Retry-After` and the `rate_limited` error code. The limiter keeps at most
100,000 active client windows and rejects new entries while that capacity is full.

The direct peer address identifies the client unless `LOREHUB_RATE_LIMIT_TRUSTED_PROXY_CIDRS` lists that peer's network.
When a trusted proxy connects, the API reads `X-Forwarded-For` from right to left and uses the first address outside the
trusted networks. List only the exact reverse proxy networks, separated by commas. The proxy must append the address of
the client that connected to it and must not allow direct access to the API port.

Example for a proxy network:

```dotenv
LOREHUB_RATE_LIMIT_TRUSTED_PROXY_CIDRS=172.30.10.0/24
```

Leave the setting empty when clients connect directly or when the proxy does not maintain `X-Forwarded-For` correctly.
