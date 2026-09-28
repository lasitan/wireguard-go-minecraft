# One-line install / upgrade for wireguard-mc (Windows 10+ x64 / ARM64, run as Administrator):
#   powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/lasitan/wireguard-go-minecraft/main/deploy/scripts/install.ps1 | iex"
# Env:
#   WG_MC_VERSION   pin a version (e.g. 2.0.3); default = latest release
#   WG_MC_GH_PROXY  download mirror prefix (e.g. https://ghfast.top/)
#   WG_MC_FORCE=1   reinstall even when already up to date
#   WG_MC_DIR       install directory (default: existing location or %ProgramFiles%\wireguard-mc)
# Keep this file ASCII-only: `irm | iex` and Windows PowerShell 5 may not decode UTF-8.

$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$repo = 'lasitan/wireguard-go-minecraft'

function Say($m) { Write-Host "==> $m" -ForegroundColor Green }
function Die($m) { Write-Host "Error: $m" -ForegroundColor Red; exit 1 }

$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) { Die 'Administrator required: open PowerShell with "Run as administrator"' }
# Machine-level value is the native CPU even when this PowerShell runs emulated (x64 on ARM64).
$nativeArch = [Environment]::GetEnvironmentVariable('PROCESSOR_ARCHITECTURE', 'Machine')
if (-not $nativeArch) { $nativeArch = $env:PROCESSOR_ARCHITECTURE }
$arch = switch ($nativeArch.ToUpper()) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { Die "Unsupported CPU '$nativeArch': only Windows 10+ x64 and ARM64 builds are published" }
}

$proxy = "$env:WG_MC_GH_PROXY"
if ($proxy -and -not $proxy.EndsWith('/')) { $proxy += '/' }

$version = "$env:WG_MC_VERSION"
if (-not $version) {
    try {
        $version = (Invoke-RestMethod -UseBasicParsing "https://api.github.com/repos/$repo/releases/latest").tag_name
    } catch {
        try {
            $r = Invoke-WebRequest -UseBasicParsing -Method Head "${proxy}https://github.com/$repo/releases/latest"
            $version = ($r.BaseResponse.ResponseUri.AbsoluteUri -split '/tag/')[-1]
        } catch { Die 'Cannot resolve latest release (set WG_MC_GH_PROXY or WG_MC_VERSION)' }
    }
}
$version = $version.TrimStart('v')

$existing = Get-Command wireguard-go -ErrorAction SilentlyContinue
$current = ''
if ($existing) {
    $current = ((& $existing.Source --version) -replace '^wireguard-go v?', '').Trim()
}
Say "windows/${arch}: latest $version, installed $(if ($current) { $current } else { 'none' })"
if ($current -eq $version -and $env:WG_MC_FORCE -ne '1') {
    Say 'Already up to date (set WG_MC_FORCE=1 to reinstall)'
    exit 0
}

$dir = if ($env:WG_MC_DIR) { $env:WG_MC_DIR } elseif ($existing) { Split-Path $existing.Source } else { Join-Path $env:ProgramFiles 'wireguard-mc' }
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$dest = Join-Path $dir 'wireguard-go.exe'
$tmp = "$dest.new"

$url = "${proxy}https://github.com/$repo/releases/download/v$version/wireguard-mc-windows10-$arch-$version.exe"
Say "downloading $url"
Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $tmp
& $tmp --version | Out-Null
if ($LASTEXITCODE -ne 0) { Remove-Item $tmp -Force; Die 'Downloaded binary does not run' }

$running = @(Get-Service -Name 'wireguard-go-*' -ErrorAction SilentlyContinue | Where-Object Status -eq 'Running')
foreach ($s in $running) { Stop-Service -Name $s.Name -Force; Say "stopped $($s.Name)" }

if (Test-Path $dest) {
    Remove-Item "$dest.old" -Force -ErrorAction SilentlyContinue
    Move-Item $dest "$dest.old" -Force
}
Move-Item $tmp $dest -Force
Remove-Item "$dest.old" -Force -ErrorAction SilentlyContinue
Say "installed $dest"

foreach ($s in $running) { Start-Service -Name $s.Name; Say "started $($s.Name)" }

$machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
if (($machinePath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', "$machinePath;$dir", 'Machine')
    $env:Path += ";$dir"
    Say "added $dir to system PATH (new terminals pick it up)"
}

Say "done: $(& $dest --version)"

function Apply-AgentBootstrap {
    if ($env:WG_MC_BOOTSTRAP -ne 'agent') { return }
    $url = "$env:WG_MC_MASTER_URL".Trim()
    $key = "$env:WG_MC_ENROLL_KEY".Trim()
    if (-not $url -or -not $key) { Die 'WG_MC_BOOTSTRAP=agent requires WG_MC_MASTER_URL and WG_MC_ENROLL_KEY' }
    $conf = Join-Path $dir 'wireguard-go-agent.json'
    $iface = if ($env:WG_MC_IFACE) { $env:WG_MC_IFACE } else { 'wg0' }
    $role = if ($env:WG_MC_ROLE) { $env:WG_MC_ROLE.ToLower() } else { 'client' }
    if ((Test-Path $conf) -and $env:WG_MC_FORCE -ne '1') {
        Say "kept existing $conf (WG_MC_FORCE=1 to overwrite)"
    } else {
        $cfg = [ordered]@{ masterUrl = $url; key = $key }
        if ($role -eq 'server') {
            $cfg['role'] = 'server'
            if ($env:WG_MC_ENDPOINT) { $cfg['endpoint'] = $env:WG_MC_ENDPOINT.Trim() }
            if ($env:WG_MC_LISTEN_PORT) { $cfg['listenPort'] = [int]$env:WG_MC_LISTEN_PORT }
        }
        [IO.File]::WriteAllText($conf, (($cfg | ConvertTo-Json) + "`n"), (New-Object Text.UTF8Encoding $false))
        Say "wrote $conf"
    }
    Say "registering autostart service ($iface)…"
    & $dest install $iface
    if ($LASTEXITCODE -ne 0) { Die "wireguard-go install failed" }
    Say "agent configured for Master $url; it will enroll on start"
}

Apply-AgentBootstrap

if (-not $current -and $env:WG_MC_BOOTSTRAP -ne 'agent') {
    Write-Host @"

Next steps:
  Master: wireguard-go install master, then edit $env:ProgramData\wireguard\wireguard-go-master.json
  Agent:  wireguard-go install, then set masterUrl + key in $env:ProgramData\wireguard\wireguard-go-agent.json
          (server node: also add "role": "server", optionally "endpoint": "PUBLIC_IP:25590")
  Or copy the one-line command from Master Web (install + configure)
Upgrade later: wireguard-go update
"@
}
