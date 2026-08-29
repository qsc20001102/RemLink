# syntax=docker/dockerfile:1.7
FROM node:24.14.0-bookworm-slim AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN npm ci --prefix frontend
COPY frontend ./frontend
RUN npm run typecheck --prefix frontend && npm run build --prefix frontend

FROM golang:1.26.7-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/frontend/engineer/dist ./frontend/engineer/dist
COPY --from=frontend /src/frontend/server/dist ./frontend/server/dist
ARG REMLINK_VERSION=1.0.0
ARG REMLINK_COMMIT=container
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
    -ldflags "-s -w -X remlink/internal/version.Version=${REMLINK_VERSION} -X remlink/internal/version.Commit=${REMLINK_COMMIT}" \
    -o /out/remlink-server ./cmd/server

FROM debian:bookworm-slim
ARG APT_FORCE_IPV4=1
ARG APT_DEBIAN_MIRROR=
ARG APT_SECURITY_MIRROR=
RUN apt_options="-o Acquire::Retries=5 -o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30"; \
    if [ -n "$APT_SECURITY_MIRROR" ]; then sed -i "s|http://deb.debian.org/debian-security|$APT_SECURITY_MIRROR|g" /etc/apt/sources.list.d/debian.sources; fi; \
    if [ -n "$APT_DEBIAN_MIRROR" ]; then sed -i "s|http://deb.debian.org/debian|$APT_DEBIAN_MIRROR|g" /etc/apt/sources.list.d/debian.sources; fi; \
    if [ -n "$APT_DEBIAN_MIRROR" ] || [ -n "$APT_SECURITY_MIRROR" ]; then \
      apt_options="$apt_options -o Acquire::http::Pipeline-Depth=0"; \
    fi; \
    if [ "$APT_FORCE_IPV4" = "1" ]; then apt_options="$apt_options -o Acquire::ForceIPv4=true"; fi; \
    apt-get $apt_options update \
    && apt-get $apt_options install -y --no-install-recommends \
      ca-certificates curl iproute2 iptables wireguard-tools \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/remlink-server /usr/local/bin/remlink-server
COPY deploy/docker/preflight.sh /usr/local/bin/remlink-preflight
COPY THIRD_PARTY_NOTICES.md /usr/share/doc/remlink/THIRD_PARTY_NOTICES.md
RUN chmod 0755 /usr/local/bin/remlink-server /usr/local/bin/remlink-preflight \
    && mkdir -p /app/data
VOLUME ["/app/data"]
EXPOSE 8080/tcp 51820/udp
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=4 \
  CMD curl --fail --silent --show-error http://127.0.0.1:8080/api/v1/server/info >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/remlink-preflight"]
CMD ["/usr/local/bin/remlink-server", "-config", "/etc/remlink/server.yaml"]
