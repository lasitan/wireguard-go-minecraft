# syntax=docker/dockerfile:1
# lasitan-cluster runtime image (WireGuard over TCP + optional Minecraft camouflage).
# Build from the repo root: docker build -f deploy/docker/Dockerfile .
# Requires at run time: --cap-add=NET_ADMIN --device=/dev/net/tun
# Config: mount /etc/lasitan-cluster with lasitan-cluster-agent.json or lasitan-cluster-master.json
ARG GO_VERSION=1.23.1
ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-bookworm AS ui
WORKDIR /repo/src/web
COPY src/web/package.json src/web/package-lock.json ./
RUN npm ci
COPY src/web/ ./
# vite outDir is ../master/ui/dist
RUN mkdir -p /repo/src/master/ui/dist && npm run build

FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /repo
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /repo/src/master/ui/dist ./src/master/ui/dist
RUN test -f src/master/ui/dist/index.html
RUN CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X golang.zx2c4.com/wireguard/src/core/version.Version=$(tr -d '\r\n\t ' < .github/build/version.txt)" \
  -o /out/lasitan-cluster ./src

FROM debian:bookworm-slim AS runtime
RUN apt-get update -qq \
  && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends \
    ca-certificates \
    iproute2 \
    iptables \
    nftables \
  && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/lasitan-cluster /usr/bin/lasitan-cluster
COPY deploy/examples/lasitan-cluster-agent.json.example /etc/lasitan-cluster/lasitan-cluster-agent.json.example
COPY deploy/examples/lasitan-cluster-master.json.example /etc/lasitan-cluster/lasitan-cluster-master.json.example

VOLUME ["/etc/lasitan-cluster"]
ENV LOG_LEVEL=error
# Foreground is required in containers (no systemd / no daemon fork).
ENTRYPOINT ["/usr/bin/lasitan-cluster", "-f"]
CMD ["lc0"]
