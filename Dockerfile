# syntax=docker/dockerfile:1
# wireguard-mc runtime image (WireGuard over TCP + optional Minecraft camouflage).
# Requires at run time: --cap-add=NET_ADMIN --device=/dev/net/tun
# Config: mount /etc/wireguard (agent/master/transport JSON or legacy conf)
ARG GO_VERSION=1.23.1
ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-bookworm AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# vite outDir is ../master/ui
RUN mkdir -p /src/master/ui && npm run build

FROM golang:${GO_VERSION}-bookworm AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/master/ui ./master/ui
RUN test -f master/ui/index.html
RUN CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.Version=${VERSION}" \
  -o /out/wireguard-go .

FROM debian:bookworm-slim AS runtime
RUN apt-get update -qq \
  && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
    ca-certificates \
    iproute2 \
    iptables \
    nftables \
  && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/wireguard-go /usr/bin/wireguard-go
COPY wireguard-go-transport.json.example /etc/wireguard/wireguard-go-transport.json
COPY wireguard-go-agent.json.example /etc/wireguard/wireguard-go-agent.json.example
COPY wireguard-go-master.json.example /etc/wireguard/wireguard-go-master.json.example
COPY debian/wg0.conf.example /etc/wireguard/wg0.conf.example

VOLUME ["/etc/wireguard"]
ENV LOG_LEVEL=error
# Foreground is required in containers (no systemd / no daemon fork).
ENTRYPOINT ["/usr/bin/wireguard-go", "-f"]
CMD ["wg0"]
