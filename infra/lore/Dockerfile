FROM debian:trixie-slim

ARG LORE_VERSION=0.8.6
ARG LORE_RELEASE_BASE=https://github.com/EpicGames/lore/releases/download
RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates curl openssl tar \
    && curl --fail --location --silent --show-error \
        "${LORE_RELEASE_BASE}/v${LORE_VERSION}/loreserver-v${LORE_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
        | tar --extract --gzip --directory /usr/local/bin ./loreserver \
    && apt-get clean
COPY infra/lore/local.toml /etc/lore/config/local.toml
COPY infra/lore/entrypoint.sh /usr/local/bin/lore-entrypoint
RUN chmod 0755 /usr/local/bin/lore-entrypoint /usr/local/bin/loreserver

VOLUME ["/data"]
EXPOSE 41337/tcp 41337/udp 41339/tcp
ENTRYPOINT ["lore-entrypoint"]
