# syntax=docker/dockerfile:1
# wireguard-mc runtime image (WireGuard over TCP + optional Minecraft camouflage).
# Requires at run time: --cap-add=NET_ADMIN --device=/dev/net/tun
# Config: mount /etc/wireguard/wg0.conf and wireguard-go-transport.json
ARG GO_VERSION=1.23.1

FROM golang:${GO_VERSION}-bookworm AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
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
COPY debian/wg0.conf.example /etc/wireguard/wg0.conf.example

VOLUME ["/etc/wireguard"]
ENV LOG_LEVEL=error
# Foreground is required in containers (no systemd / no daemon fork).
ENTRYPOINT ["/usr/bin/wireguard-go", "-f"]
CMD ["wg0"]
