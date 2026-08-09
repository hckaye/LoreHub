FROM golang:1.26.5-trixie

ARG LORE_CLIENT_VERSION=0.8.6
ARG LORE_SDK_VERSION=0.8.5
ARG LORE_RELEASE_BASE=https://github.com/EpicGames/lore/releases/download

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates curl tar \
    && mkdir -p /opt/lore \
    && curl --fail --location --silent --show-error \
        "${LORE_RELEASE_BASE}/v${LORE_SDK_VERSION}/liblore-v${LORE_SDK_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
        | tar --extract --gzip --directory /opt/lore \
    && curl --fail --location --silent --show-error \
        "${LORE_RELEASE_BASE}/v${LORE_CLIENT_VERSION}/lore-v${LORE_CLIENT_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
        | tar --extract --gzip --directory /opt/lore \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src/services/api
COPY services/api/go.mod services/api/go.sum ./
RUN go mod download
COPY services/api ./
COPY scripts/lore-merge-fixture.sh /usr/local/bin/lore-fixture
RUN chmod 0755 /usr/local/bin/lore-fixture

RUN chmod 0755 /opt/lore/lore && ln -s /opt/lore/lore /usr/local/bin/lore

ENV LORE_LIB_PATH=/opt/lore/liblore.so
