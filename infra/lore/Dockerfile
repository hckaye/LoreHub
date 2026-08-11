# syntax=docker/dockerfile:1.7

FROM rust:1.97.1-bookworm AS lore-build

RUN apt-get update \
    && apt-get install --yes --no-install-recommends git protobuf-compiler libprotobuf-dev pkg-config libssl-dev \
    && rm -rf /var/lib/apt/lists/* \
    && git clone --branch v0.8.6 --depth 1 https://github.com/EpicGames/lore.git /src/lore
WORKDIR /src/lore

# Compile the unmodified upstream workspace before copying the hook. Docker can
# reuse this layer when only the hook changes, so dependency and stock Lore
# compilation are not repeated for every policy iteration.
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    cargo build --locked --release --package lore-server --bin loreserver \
        --package lore-client --bin lore

COPY infra/lore/branch-name-hook-context.patch /tmp/branch-name-hook-context.patch
COPY infra/lore/auth-backend-url.patch /tmp/auth-backend-url.patch
RUN git apply --unidiff-zero --check /tmp/branch-name-hook-context.patch \
    && git apply --unidiff-zero /tmp/branch-name-hook-context.patch \
    && git apply --check /tmp/auth-backend-url.patch \
    && git apply /tmp/auth-backend-url.patch
COPY infra/lore/lorehub_policy_hook.rs /src/lore/lore-server/src/hooks/lorehub_policy.rs
COPY infra/lore/lorehub_policy_tls_test_support.rs \
    /src/lore/lore-server/src/hooks/lorehub_policy/tls_test_support.rs

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    cargo build --locked --release --package lore-server --bin loreserver \
    && cargo build --locked --release --package lore-client --bin lore \
    && install -Dm755 target/release/loreserver /out/loreserver \
    && install -Dm755 target/release/lore /out/lore

FROM lore-build AS lore-tests

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    cargo test --locked --package lore-server lorehub_policy --no-run

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    cargo test --locked --package lore-server lorehub_policy -- --test-threads=1

FROM debian:trixie-slim

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates curl \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
COPY --from=lore-build /out/loreserver /usr/local/bin/loreserver
COPY --from=lore-build /out/lore /usr/local/bin/lore
COPY infra/lore/local.toml /etc/lore/config/local.toml
COPY infra/lore/production.toml /etc/lore/config/production.toml
COPY infra/lore/entrypoint.sh /usr/local/bin/lore-entrypoint
RUN chmod 0755 /usr/local/bin/lore-entrypoint /usr/local/bin/loreserver

VOLUME ["/data"]
EXPOSE 41337/tcp 41337/udp 41339/tcp
ENTRYPOINT ["lore-entrypoint"]
