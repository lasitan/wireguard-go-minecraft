# Build and package Linux amd64 server + client config bundles (binary + examples).
# Prefer apt (.deb) for installs. Usage (from repo root):
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
    (Join-Path $ServerDir "usr\bin"), `
    (Join-Path $ServerDir "etc\wireguard"), `
    (Join-Path $ClientDir "usr\bin"), `
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

Copy-Item $BinPath (Join-Path $ServerDir "usr\bin\$BinName")
Copy-Item $BinPath (Join-Path $ClientDir "usr\bin\$BinName")
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
PersistentKeepalive = 5
'@

Write-UnixFile (Join-Path $ServerDir "README.txt") @'
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
'@

Write-UnixFile (Join-Path $ClientDir "README.txt") @'
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
