FROM rust:1.91-bookworm AS lore-build

ARG LORE_VERSION=0.8.6
ARG LORE_COMMIT=4fa9870b86efc17a54cb7ead7b0c0364b36e8348
RUN apt-get update \
    && apt-get install --yes --no-install-recommends git protobuf-compiler libprotobuf-dev pkg-config libssl-dev \
    && rm -rf /var/lib/apt/lists/* \
    && git clone --branch "v${LORE_VERSION}" --depth 1 https://github.com/EpicGames/lore.git /src/lore \
    && cd /src/lore \
    && test "$(git rev-parse HEAD)" = "${LORE_COMMIT}"
COPY infra/lore/lorehub_policy_hook.rs /src/lore/lore-server/src/hooks/lorehub_policy.rs
RUN cd /src/lore \
    && cargo build --locked --release --package lore-server --bin loreserver \
    && install -Dm755 target/release/loreserver /out/loreserver

FROM debian:trixie-slim

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates curl \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
COPY --from=lore-build /out/loreserver /usr/local/bin/loreserver
COPY infra/lore/local.toml /etc/lore/config/local.toml
COPY infra/lore/entrypoint.sh /usr/local/bin/lore-entrypoint
RUN chmod 0755 /usr/local/bin/lore-entrypoint /usr/local/bin/loreserver

VOLUME ["/data"]
EXPOSE 41337/tcp 41337/udp 41339/tcp
ENTRYPOINT ["lore-entrypoint"]
