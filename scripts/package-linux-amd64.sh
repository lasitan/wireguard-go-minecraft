#!/usr/bin/env bash
# Build and package Linux amd64 server + client config bundles (binary + examples).
# Prefer apt (.deb) for installs. For Docker images see scripts/package-docker.sh
# and workflow_dispatch input build_docker / Variable ENABLE_DOCKER_RELEASE.
# Usage (from repo root):
#   bash scripts/package-linux-amd64.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUT_ROOT="$ROOT/dist/wireguard-mc-linux-amd64"
SERVER_DIR="$OUT_ROOT/server"
CLIENT_DIR="$OUT_ROOT/client"
BIN_NAME="wireguard-go"

echo "==> Cleaning $OUT_ROOT"
rm -rf "$OUT_ROOT"
mkdir -p \
  "$SERVER_DIR/usr/bin" "$SERVER_DIR/etc/wireguard" \
  "$CLIENT_DIR/usr/bin" "$CLIENT_DIR/etc/wireguard"

echo "==> Building linux/amd64"
export GOOS=linux GOARCH=amd64 CGO_ENABLED=0
go build -trimpath -ldflags "-s -w" -o "$OUT_ROOT/$BIN_NAME" .

cp "$OUT_ROOT/$BIN_NAME" "$SERVER_DIR/usr/bin/$BIN_NAME"
cp "$OUT_ROOT/$BIN_NAME" "$CLIENT_DIR/usr/bin/$BIN_NAME"
rm -f "$OUT_ROOT/$BIN_NAME"
chmod a+x "$SERVER_DIR/usr/bin/$BIN_NAME" "$CLIENT_DIR/usr/bin/$BIN_NAME"

TRANSPORT_SRC="$ROOT/wireguard-go-transport.json"
[[ -f "$TRANSPORT_SRC" ]] || TRANSPORT_SRC="$ROOT/wireguard-go-transport.json.example"
cp "$TRANSPORT_SRC" "$SERVER_DIR/etc/wireguard/wireguard-go-transport.json"
cp "$TRANSPORT_SRC" "$CLIENT_DIR/etc/wireguard/wireguard-go-transport.json"

cat > "$SERVER_DIR/etc/wireguard/wg0.conf.example" <<'EOF'
[Interface]
PrivateKey = SERVER_PRIVATE_KEY_BASE64
Address = 10.0.0.1/24
ListenPort = 25565
MTU = 1420

[Peer]
PublicKey = CLIENT_PUBLIC_KEY_BASE64
AllowedIPs = 10.0.0.2/32
# Built-in TCP proxy: public listen -> client VPN IP
#   25565                         -> 10.0.0.2:25565
#   8080:80                       -> 10.0.0.2:80
#   25565:10.0.0.2:25565
#   0.0.0.0:8443:10.0.0.2:443
# Multiple forwards: comma (or semicolon) separated
ForwardTCP = 25565, 8080:80, 8443:10.0.0.2:443
# Same syntax for UDP (e.g. game voice / custom UDP services)
ForwardUDP = 19132, 25565
# Optional shell hooks (run as root). Env: WG_PEER, WG_PEER_HOST, WG_ALLOWED_IP
# OnUp = echo peer up $WG_PEER_HOST
# OnDown = echo peer down $WG_PEER_HOST
EOF

cat > "$CLIENT_DIR/etc/wireguard/wg0.conf.example" <<'EOF'
[Interface]
PrivateKey = CLIENT_PRIVATE_KEY_BASE64
Address = 10.0.0.2/24
MTU = 1420

[Peer]
PublicKey = SERVER_PUBLIC_KEY_BASE64
Endpoint = SERVER_PUBLIC_IP:25565
AllowedIPs = 10.0.0.0/24
PersistentKeepalive = 5
EOF

cat > "$SERVER_DIR/README.txt" <<'EOF'
wireguard-go TCP+MC — SERVER (prefer apt: wireguard-mc)

1) Install the .deb, or copy usr/bin/wireguard-go and etc/wireguard/* into place
2) Edit:
     /etc/wireguard/wg0.conf
     /etc/wireguard/wireguard-go-transport.json   # same loginPluginSecret as client
3) Enable systemd service:
     sudo wireguard-go install
4) Status:
     systemctl status wireguard-go@wg0
     journalctl -u wireguard-go@wg0 -f

Remove service:
     sudo wireguard-go uninstall
     sudo wireguard-go uninstall --purge

Both ends MUST use this custom binary (not kernel WireGuard / standard UDP).
EOF

cat > "$CLIENT_DIR/README.txt" <<'EOF'
wireguard-go TCP+MC — CLIENT (prefer apt: wireguard-mc)

1) Install the .deb, or copy usr/bin/wireguard-go and etc/wireguard/* into place
2) Edit:
     /etc/wireguard/wg0.conf          # set Endpoint = server_ip:port
     /etc/wireguard/wireguard-go-transport.json   # same loginPluginSecret as server
3) Enable systemd service:
     sudo wireguard-go install
4) Status:
     systemctl status wireguard-go@wg0

Remove service:
     sudo wireguard-go uninstall
     sudo wireguard-go uninstall --purge

Ping server tunnel IP (e.g. 10.0.0.1) to verify.
EOF

echo "==> Creating tar.gz archives"
(
  cd "$OUT_ROOT"
  tar -czf wireguard-mc-linux-amd64-server.tar.gz server
  tar -czf wireguard-mc-linux-amd64-client.tar.gz client
)

echo
echo "Done."
echo "  Server: $OUT_ROOT/wireguard-mc-linux-amd64-server.tar.gz"
echo "  Client: $OUT_ROOT/wireguard-mc-linux-amd64-client.tar.gz"
