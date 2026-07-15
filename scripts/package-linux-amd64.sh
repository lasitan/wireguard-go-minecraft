#!/usr/bin/env bash
# Build and package Linux amd64 server + client bundles.
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
  "$SERVER_DIR/usr/local/bin" "$SERVER_DIR/etc/wireguard" \
  "$CLIENT_DIR/usr/local/bin" "$CLIENT_DIR/etc/wireguard"

echo "==> Building linux/amd64"
export GOOS=linux GOARCH=amd64 CGO_ENABLED=0
go build -trimpath -ldflags "-s -w" -o "$OUT_ROOT/$BIN_NAME" .

cp "$OUT_ROOT/$BIN_NAME" "$SERVER_DIR/usr/local/bin/$BIN_NAME"
cp "$OUT_ROOT/$BIN_NAME" "$CLIENT_DIR/usr/local/bin/$BIN_NAME"
rm -f "$OUT_ROOT/$BIN_NAME"

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
PersistentKeepalive = 25
EOF

cat > "$SERVER_DIR/install.sh" <<'EOF'
#!/bin/sh
set -e
cd "$(dirname "$0")"
install -d /usr/local/bin /etc/wireguard
install -m 0755 usr/local/bin/wireguard-go /usr/local/bin/wireguard-go
install -m 0644 etc/wireguard/wireguard-go-transport.json /etc/wireguard/wireguard-go-transport.json
if [ ! -f /etc/wireguard/wg0.conf ]; then
  install -m 0600 etc/wireguard/wg0.conf.example /etc/wireguard/wg0.conf
  echo "Created /etc/wireguard/wg0.conf from example 鈥?edit keys before start."
else
  echo "Kept existing /etc/wireguard/wg0.conf"
fi
echo "Installed. Edit /etc/wireguard/*.conf then: wireguard-go -f wg0"
EOF
cp "$SERVER_DIR/install.sh" "$CLIENT_DIR/install.sh"
chmod +x "$SERVER_DIR/install.sh" "$CLIENT_DIR/install.sh" \
  "$SERVER_DIR/usr/local/bin/$BIN_NAME" "$CLIENT_DIR/usr/local/bin/$BIN_NAME"

cat > "$SERVER_DIR/uninstall.sh" <<'EOF'
#!/bin/sh
set -e

# Stop running daemon if present
if pgrep -x wireguard-go >/dev/null 2>&1; then
  echo "Stopping wireguard-go..."
  pkill -x wireguard-go || true
  sleep 1
fi

# Bring down TUN if still around
if ip link show wg0 >/dev/null 2>&1; then
  echo "Removing interface wg0..."
  ip link set wg0 down 2>/dev/null || true
  ip link delete wg0 2>/dev/null || true
fi

rm -f /usr/local/bin/wireguard-go
rm -f /etc/wireguard/wireguard-go-transport.json

# Keep wg0.conf by default (keys). Pass --purge to delete it too.
if [ "${1:-}" = "--purge" ]; then
  rm -f /etc/wireguard/wg0.conf
  echo "Removed /etc/wireguard/wg0.conf"
else
  echo "Kept /etc/wireguard/wg0.conf (use --purge to delete)"
fi

# Remove empty UAPI socket dir leftovers for this iface
rm -f /var/run/wireguard/wg0.sock 2>/dev/null || true

echo "Uninstalled wireguard-go (TCP+MC)."
EOF
cp "$SERVER_DIR/uninstall.sh" "$CLIENT_DIR/uninstall.sh"
chmod +x "$SERVER_DIR/uninstall.sh" "$CLIENT_DIR/uninstall.sh"

cat > "$SERVER_DIR/README.txt" <<'EOF'
wireguard-go TCP+MC Linux amd64 鈥?SERVER

1) sudo ./install.sh
2) Edit:
     /etc/wireguard/wg0.conf
     /etc/wireguard/wireguard-go-transport.json   # same loginPluginSecret as client
3) Open firewall TCP ListenPort (default example: 25565/tcp)
4) Start:
     sudo LOG_LEVEL=verbose /usr/local/bin/wireguard-go -f wg0
   Address/MTU from wg0.conf are applied automatically.

Uninstall:
     sudo ./uninstall.sh
     sudo ./uninstall.sh --purge

Both ends MUST use this custom binary (not kernel WireGuard / standard UDP).
EOF

cat > "$CLIENT_DIR/README.txt" <<'EOF'
wireguard-go TCP+MC Linux amd64 鈥?CLIENT

1) sudo ./install.sh
2) Edit:
     /etc/wireguard/wg0.conf          # set Endpoint = server_ip:port
     /etc/wireguard/wireguard-go-transport.json   # same loginPluginSecret as server
3) Start:
     sudo LOG_LEVEL=verbose /usr/local/bin/wireguard-go -f wg0
   Address/MTU from wg0.conf are applied automatically.



Uninstall:
     sudo ./uninstall.sh
     sudo ./uninstall.sh --purge

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
