# One-line install / upgrade for wireguard-mc (Windows 10+ x64, run as Administrator):
#   powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/lasitan/wireguard-go-minecraft/main/scripts/install.ps1 | iex"
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
if (-not [Environment]::Is64BitOperatingSystem) { Die 'Only Windows 10+ x64 builds are published' }

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
Say "latest $version, installed $(if ($current) { $current } else { 'none' })"
if ($current -eq $version -and $env:WG_MC_FORCE -ne '1') {
    Say 'Already up to date (set WG_MC_FORCE=1 to reinstall)'
    exit 0
}

$dir = if ($env:WG_MC_DIR) { $env:WG_MC_DIR } elseif ($existing) { Split-Path $existing.Source } else { Join-Path $env:ProgramFiles 'wireguard-mc' }
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$dest = Join-Path $dir 'wireguard-go.exe'
$tmp = "$dest.new"

$url = "${proxy}https://github.com/$repo/releases/download/v$version/wireguard-mc-windows10-amd64-$version.exe"
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
if (-not $current) {
    Write-Host @"

Next steps:
  Master: wireguard-go install master, then edit $env:ProgramData\wireguard\wireguard-go-master.json
  Agent:  wireguard-go install, then set masterUrl + key in $env:ProgramData\wireguard\wireguard-go-agent.json
Upgrade later: wireguard-go update
"@
}
