# syntax=docker/dockerfile:1.7

ARG VM_VERSION=v1.152.0

FROM --platform=$BUILDPLATFORM node:22-alpine@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS go
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/embed.go
COPY --from=web /src/web/dist web/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/homedeck ./cmd/homedeck

FROM victoriametrics/victoria-metrics:${VM_VERSION}@sha256:86ca5fdb6d87d56ba047b044039019ba2bd9042b36e35f6ea34e437b6c825cef AS vm

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN apk add --no-cache tini ca-certificates \
    && mkdir -p /data \
    && chown 1000:1000 /data
COPY --from=vm /victoria-metrics-prod /usr/local/bin/victoria-metrics
COPY --from=go /out/homedeck /usr/local/bin/homedeck
ENV HOMEDECK_DATA_DIR=/data \
    HOMEDECK_LISTEN=:8080 \
    HOMEDECK_VM_BINARY=/usr/local/bin/victoria-metrics
USER 1000:1000
VOLUME /data
EXPOSE 8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 CMD ["/usr/local/bin/homedeck", "healthcheck"]
ENTRYPOINT ["/sbin/tini", "-g", "--", "/usr/local/bin/homedeck"]
CMD ["serve"]
