FROM golang:1.26.5-trixie AS build

WORKDIR /src/services/api
COPY services/api/go.mod services/api/go.sum ./
RUN go mod download
COPY services/api ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /out/lorehub ./cmd/lorehub

FROM debian:trixie-slim AS lore-assets

ARG LORE_CLIENT_VERSION=0.8.6
ARG LORE_SDK_VERSION=0.8.5
ARG ACT_VERSION=0.2.89
ARG LORE_RELEASE_BASE=https://github.com/EpicGames/lore/releases/download
ARG ACT_RELEASE_BASE=https://github.com/nektos/act/releases/download
RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates curl tar \
    && mkdir -p /assets/lore /assets/act \
    && curl --fail --location --silent --show-error \
        "${LORE_RELEASE_BASE}/v${LORE_SDK_VERSION}/liblore-v${LORE_SDK_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
        | tar --extract --gzip --directory /assets/lore \
    && curl --fail --location --silent --show-error \
        "${LORE_RELEASE_BASE}/v${LORE_CLIENT_VERSION}/lore-v${LORE_CLIENT_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
        | tar --extract --gzip --directory /assets/lore \
    && curl --fail --location --silent --show-error \
        "${ACT_RELEASE_BASE}/v${ACT_VERSION}/act_Linux_x86_64.tar.gz" \
        | tar --extract --gzip --directory /assets/act act

FROM debian:trixie-slim

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates curl \
    && groupadd --system lorehub \
    && useradd --system --gid lorehub --create-home lorehub \
    && mkdir -p /var/lib/lorehub \
    && chown lorehub:lorehub /var/lib/lorehub \
    && apt-get clean
COPY --from=build /out/lorehub /usr/local/bin/lorehub
COPY --from=lore-assets /assets/lore/liblore.so /opt/lore/liblore.so
COPY --from=lore-assets /assets/lore/lore /usr/local/bin/lore
COPY --from=lore-assets /assets/lore/LICENSE.txt /opt/lore/LICENSE.txt
COPY --from=lore-assets /assets/lore/THIRD-PARTY-NOTICES.txt /opt/lore/THIRD-PARTY-NOTICES.txt
COPY --from=lore-assets /assets/act/act /usr/local/bin/act
COPY infra/api/entrypoint.sh /usr/local/bin/lorehub-entrypoint
RUN chmod 0755 /usr/local/bin/lore /usr/local/bin/act /usr/local/bin/lorehub-entrypoint

ENV LORE_LIB_PATH=/opt/lore/liblore.so
ENV LOREHUB_LORE_CACHE_DIR=/var/lib/lorehub/repositories
ENV LOREHUB_RUNNER_LOG_DIR=/var/lib/lorehub/runner-logs
ENV LOREHUB_RUNNER_ARTIFACT_DIR=/var/lib/lorehub/runner-artifacts
ENV LOREHUB_RUNNER_WORK_DIR=/var/lib/lorehub/runner-work
EXPOSE 8080
ENTRYPOINT ["lorehub-entrypoint"]
CMD ["serve"]
