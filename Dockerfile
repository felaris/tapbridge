# syntax=docker/dockerfile:1

# TapBridge headless image: bundles pcscd + libccid so a USB reader passed in
# with --device works out of the box. See docs/docker.md.
#
#   docker run -d --name tapbridge --device /dev/bus/usb \
#     -p 127.0.0.1:8765:8765 felaris/tapbridge

ARG GO_VERSION=1.26
ARG UBUNTU_VERSION=24.04

# ── Build ────────────────────────────────────────────────────────────────────
FROM golang:${GO_VERSION} AS go

FROM ubuntu:${UBUNTU_VERSION} AS build
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH \
    GOTOOLCHAIN=local
RUN apt-get update \
 && apt-get install -y --no-install-recommends gcc libc6-dev pkg-config libpcsclite-dev ca-certificates \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
# -tags notray drops the GTK/AppIndicator tray, leaving only libpcsclite.
RUN --mount=type=cache,target=/root/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -tags notray -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/tapbridge ./cmd/tapbridge

# ── Runtime ──────────────────────────────────────────────────────────────────
FROM ubuntu:${UBUNTU_VERSION}
ARG VERSION=dev
LABEL org.opencontainers.image.title="TapBridge" \
      org.opencontainers.image.description="Bridge a USB PC/SC NFC reader to web apps over a local WebSocket" \
      org.opencontainers.image.source="https://github.com/felaris/tapbridge" \
      org.opencontainers.image.documentation="https://github.com/felaris/tapbridge/blob/main/docs/docker.md" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"

RUN apt-get update \
 && apt-get install -y --no-install-recommends pcscd libccid libpcsclite1 \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 10001 --user-group --home-dir /data --shell /usr/sbin/nologin tapbridge \
 && mkdir -p /data /run/pcscd \
 && chown tapbridge:tapbridge /data

COPY --from=build /out/tapbridge /usr/local/bin/tapbridge
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh

# Listen on all interfaces *inside* the container so the published port works.
# Keep it private on the host by publishing to loopback: -p 127.0.0.1:8765:8765
ENV TAPBRIDGE_HOST=0.0.0.0 \
    TAPBRIDGE_HEADLESS=1 \
    XDG_CONFIG_HOME=/data

EXPOSE 8765
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["tapbridge", "--healthcheck"]

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
