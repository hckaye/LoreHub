FROM debian:trixie-slim

RUN apt-get update \
    && apt-get install --yes --no-install-recommends openssl \
    && rm -rf /var/lib/apt/lists/*
COPY infra/auth-tls/entrypoint.sh /usr/local/bin/auth-tls-entrypoint
RUN chmod 0755 /usr/local/bin/auth-tls-entrypoint
ENTRYPOINT ["auth-tls-entrypoint"]
