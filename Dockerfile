# syntax=docker/dockerfile:1.7

ARG VM_VERSION=v1.152.0

# UI собирается Bun; версия совпадает с packageManager в web/package.json.
FROM --platform=$BUILDPLATFORM oven/bun:1.4.2-alpine@sha256:d888c0ae6c86d7866ff10c5aafdd9077b36aee6455b33dd270fb93c0dd5cef6f AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock web/bunfig.toml ./
RUN --mount=type=cache,target=/root/.bun/install/cache bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS go
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY drivers/ drivers/
COPY web/embed.go web/embed.go
COPY --from=web /src/web/dist web/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/homedeck ./cmd/homedeck

FROM victoriametrics/victoria-metrics:${VM_VERSION}@sha256:86ca5fdb6d87d56ba047b044039019ba2bd9042b36e35f6ea34e437b6c825cef AS vm

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN apk add --no-cache tini ca-certificates \
    && mkdir -p /data
COPY --from=vm /victoria-metrics-prod /usr/local/bin/victoria-metrics
COPY --from=go /out/homedeck /usr/local/bin/homedeck
ENV HOMEDECK_DATA_DIR=/data \
    HOMEDECK_LISTEN=:8080 \
    HOMEDECK_VM_BINARY=/usr/local/bin/victoria-metrics
# Без USER: процесс работает с правами того, кто запустил контейнер. В rootless Podman
# и Docker это пользователь хоста, в rootful — root. HomeDeck пишет в любой указанный
# каталог данных, куда у этого пользователя есть доступ. Привилегии ограничивает
# compose.yaml (cap_drop, no-new-privileges, read_only).
VOLUME /data
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 CMD ["/usr/local/bin/homedeck", "healthcheck"]
ENTRYPOINT ["/sbin/tini", "-g", "--", "/usr/local/bin/homedeck"]
CMD ["serve"]
