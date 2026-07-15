# Build and package Linux amd64 server + client bundles.
# Usage (from repo root):
#   powershell -ExecutionPolicy Bypass -File .\scripts\package-linux-amd64.ps1

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $Root

function Write-UnixFile([string]$Path, [string]$Content) {
    $dir = Split-Path $Path -Parent
    if ($dir -and -not (Test-Path $dir)) {
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
    }
    $normalized = ($Content -replace "`r`n", "`n").TrimEnd() + "`n"
    [System.IO.File]::WriteAllText($Path, $normalized, [System.Text.UTF8Encoding]::new($false))
}

$OutRoot = Join-Path $Root "dist\wireguard-mc-linux-amd64"
$ServerDir = Join-Path $OutRoot "server"
$ClientDir = Join-Path $OutRoot "client"
$BinName = "wireguard-go"

Write-Host "==> Cleaning $OutRoot"
if (Test-Path $OutRoot) {
    Remove-Item -Recurse -Force $OutRoot
}
New-Item -ItemType Directory -Force -Path `
    (Join-Path $ServerDir "usr\local\bin"), `
    (Join-Path $ServerDir "etc\wireguard"), `
    (Join-Path $ClientDir "usr\local\bin"), `
    (Join-Path $ClientDir "etc\wireguard") | Out-Null

Write-Host "==> Building linux/amd64"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
$BinPath = Join-Path $OutRoot $BinName
go build -trimpath -ldflags "-s -w" -o $BinPath .
if ($LASTEXITCODE -ne 0) {
    throw "go build failed"
}

Copy-Item $BinPath (Join-Path $ServerDir "usr\local\bin\$BinName")
Copy-Item $BinPath (Join-Path $ClientDir "usr\local\bin\$BinName")
Remove-Item $BinPath

$TransportSrc = Join-Path $Root "wireguard-go-transport.json"
if (-not (Test-Path $TransportSrc)) {
    $TransportSrc = Join-Path $Root "wireguard-go-transport.json.example"
}
Copy-Item $TransportSrc (Join-Path $ServerDir "etc\wireguard\wireguard-go-transport.json")
Copy-Item $TransportSrc (Join-Path $ClientDir "etc\wireguard\wireguard-go-transport.json")

Write-UnixFile (Join-Path $ServerDir "etc\wireguard\wg0.conf.example") @'
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
'@

Write-UnixFile (Join-Path $ClientDir "etc\wireguard\wg0.conf.example") @'
[Interface]
PrivateKey = CLIENT_PRIVATE_KEY_BASE64
Address = 10.0.0.2/24
MTU = 1420

[Peer]
PublicKey = SERVER_PUBLIC_KEY_BASE64
Endpoint = SERVER_PUBLIC_IP:25565
AllowedIPs = 10.0.0.0/24
PersistentKeepalive = 25
'@

$InstallSh = @'
#!/bin/sh
set -e
cd "$(dirname "$0")"
install -d /usr/local/bin /etc/wireguard
install -m 0755 usr/local/bin/wireguard-go /usr/local/bin/wireguard-go
install -m 0644 etc/wireguard/wireguard-go-transport.json /etc/wireguard/wireguard-go-transport.json
if [ ! -f /etc/wireguard/wg0.conf ]; then
  install -m 0600 etc/wireguard/wg0.conf.example /etc/wireguard/wg0.conf
  echo "Created /etc/wireguard/wg0.conf from example — edit keys before start."
else
  echo "Kept existing /etc/wireguard/wg0.conf"
fi
echo "Installed. Edit /etc/wireguard/*.conf then: wireguard-go -f wg0"
'@
Write-UnixFile (Join-Path $ServerDir "install.sh") $InstallSh
Write-UnixFile (Join-Path $ClientDir "install.sh") $InstallSh

$UninstallSh = @'
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
'@
Write-UnixFile (Join-Path $ServerDir "uninstall.sh") $UninstallSh
Write-UnixFile (Join-Path $ClientDir "uninstall.sh") $UninstallSh

Write-UnixFile (Join-Path $ServerDir "README.txt") @'
wireguard-go TCP+MC Linux amd64 — SERVER

1) sudo ./install.sh
2) Edit:
     /etc/wireguard/wg0.conf
     /etc/wireguard/wireguard-go-transport.json   # same loginPluginSecret as client
3) Open firewall TCP ListenPort (default example: 25565/tcp)
4) Start:
     sudo LOG_LEVEL=verbose /usr/local/bin/wireguard-go -f wg0
   Address/MTU from wg0.conf are applied automatically.

Uninstall:
     sudo ./uninstall.sh          # keep wg0.conf
     sudo ./uninstall.sh --purge  # also delete wg0.conf

Both ends MUST use this custom binary (not kernel WireGuard / standard UDP).
'@

Write-UnixFile (Join-Path $ClientDir "README.txt") @'
wireguard-go TCP+MC Linux amd64 — CLIENT

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
'@

Write-Host "==> Creating tar.gz archives"
function New-LinuxTarGz([string]$SourceDir, [string]$ArchivePath) {
    if (-not (Get-Command tar -ErrorAction SilentlyContinue)) {
        throw "tar not found; install Windows tar or use WSL"
    }
    $parent = Split-Path $SourceDir -Parent
    $name = Split-Path $SourceDir -Leaf
    Push-Location $parent
    try {
        if (Test-Path $ArchivePath) { Remove-Item -Force $ArchivePath }
        tar -czf $ArchivePath $name
    } finally {
        Pop-Location
    }
}

$ServerTar = Join-Path $OutRoot "wireguard-mc-linux-amd64-server.tar.gz"
$ClientTar = Join-Path $OutRoot "wireguard-mc-linux-amd64-client.tar.gz"
New-LinuxTarGz $ServerDir $ServerTar
New-LinuxTarGz $ClientDir $ClientTar

Write-Host ""
Write-Host "Done."
Write-Host "  Server: $ServerTar"
Write-Host "  Client: $ClientTar"
Write-Host "  Trees : $ServerDir"
Write-Host "          $ClientDir"
