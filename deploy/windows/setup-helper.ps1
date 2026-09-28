# Service / PATH steps for the wireguard-mc NSIS installer (deploy/windows/installer.nsi).
# Keep this file ASCII-only: Windows PowerShell 5 reads it without a BOM.
param(
    [Parameter(Mandatory)][ValidateSet('PreInstall', 'PostInstall', 'AddPath', 'WriteConfig', 'Autostart', 'Uninstall')][string]$Action,
    [Parameter(Mandatory)][string]$Dir,
    # WriteConfig: UTF-16 key=value lines from the installer's mode page (deleted after use).
    [string]$InputFile
)
$ErrorActionPreference = 'Continue'
$Dir = $Dir.TrimEnd('\')
$exe = Join-Path $Dir 'wireguard-go.exe'
$stateFile = Join-Path $Dir '.running-services'

function Say($m) { Write-Output "wireguard-mc: $m" }

function Get-OwnServices {
    @(Get-CimInstance Win32_Service -Filter "Name LIKE 'wireguard-go-%'" -ErrorAction SilentlyContinue)
}

function Stop-OwnService($name) {
    Stop-Service -Name $name -Force -ErrorAction SilentlyContinue
    $deadline = (Get-Date).AddSeconds(15)
    while ((Get-Service -Name $name -ErrorAction SilentlyContinue).Status -ne 'Stopped' -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 200
    }
    Say "stopped $name"
}

function Get-MachinePath { @([Environment]::GetEnvironmentVariable('Path', 'Machine') -split ';' | Where-Object { $_ }) }

function Invoke-Exe {
    # 2>&1 + stringify: PowerShell 5 would otherwise wrap native stderr lines as error records.
    & $exe @args 2>&1 | ForEach-Object { [Console]::Out.WriteLine("wireguard-mc: $_") }
}

switch ($Action) {
    'PreInstall' {
        $running = @(Get-OwnServices | Where-Object State -eq 'Running' | ForEach-Object Name)
        foreach ($n in $running) { Stop-OwnService $n }
        [IO.File]::WriteAllLines($stateFile, [string[]]$running)
        Remove-Item "$exe.old" -Force -ErrorAction SilentlyContinue
    }
    'PostInstall' {
        # Services created by an install in another directory must follow the new binary.
        foreach ($svc in Get-OwnServices) {
            $svcArgs = $svc.PathName -replace '^\s*("[^"]*"|\S+)\s*', ''
            $want = ('"{0}" {1}' -f $exe, $svcArgs).TrimEnd()
            if ($svc.PathName -ne $want) {
                $r = Invoke-CimMethod -InputObject $svc -MethodName Change -Arguments @{ PathName = $want }
                if ($r.ReturnValue -eq 0) { Say "$($svc.Name) -> $want" } else { Say "update $($svc.Name) failed: $($r.ReturnValue)" }
            }
        }
        if (Test-Path $stateFile) {
            foreach ($n in Get-Content $stateFile) {
                if (-not $n) { continue }
                Start-Service -Name $n -ErrorAction SilentlyContinue
                Say "started $n"
            }
            Remove-Item $stateFile -Force
        }
    }
    'AddPath' {
        $path = Get-MachinePath
        if ($path -notcontains $Dir) {
            [Environment]::SetEnvironmentVariable('Path', (($path + $Dir) -join ';'), 'Machine')
            Say "added $Dir to system PATH"
        }
    }
    'WriteConfig' {
        $kv = @{}
        foreach ($line in Get-Content -LiteralPath $InputFile -Encoding Unicode) {
            $i = $line.IndexOf('=')
            if ($i -gt 0) { $kv[$line.Substring(0, $i)] = $line.Substring($i + 1) }
        }
        Remove-Item -LiteralPath $InputFile -Force
        switch ($kv['mode']) {
            'agent' {
                $file = 'wireguard-go-agent.json'
                $cfg = [ordered]@{ masterUrl = $kv['masterUrl']; key = $kv['key'] }
                if ($kv['role']) { $cfg['role'] = $kv['role'] }
                if ($kv['endpoint']) { $cfg['endpoint'] = $kv['endpoint'].Trim() }
            }
            'master' {
                $file = 'wireguard-go-master.json'
                $cfg = [ordered]@{ listen = $kv['listen']; adminPassword = $kv['adminPassword']; dataDir = (Join-Path $Dir 'master-data') }
            }
            default { exit 0 }
        }
        $path = Join-Path $Dir $file
        if (Test-Path $path) { Say "kept existing $path"; exit 0 }
        [IO.File]::WriteAllText($path, (($cfg | ConvertTo-Json) + "`n"), (New-Object Text.UTF8Encoding $false))
        Say "wrote $path"
    }
    'Autostart' {
        # Registers an automatic-start (boot) service for the mode whose config sits in $Dir.
        $masterCfg = Join-Path $Dir 'wireguard-go-master.json'
        $agentCfg = Join-Path $Dir 'wireguard-go-agent.json'
        $hasMaster = Test-Path $masterCfg
        $hasAgent = Test-Path $agentCfg
        if ($hasMaster -and $hasAgent) {
            Say "both master and agent configs exist in $Dir; autostart skipped (keep only one, then run: wireguard-go install [master])"
            exit 0
        }
        if ($hasMaster) {
            Invoke-Exe install master
        } elseif ($hasAgent) {
            $iface = 'wg0'
            try {
                $j = Get-Content -LiteralPath $agentCfg -Raw | ConvertFrom-Json
                if ($j.interface) { $iface = $j.interface }
            } catch {}
            Invoke-Exe install $iface
        } else {
            Say "no config in $Dir; autostart skipped (add a config, then run: wireguard-go install [master])"
            exit 0
        }
        $code = $LASTEXITCODE
        if ($code -ne 0) { Say "autostart setup exited with $code (service may be registered but not running; check the config)" }
    }
    'Uninstall' {
        foreach ($svc in Get-OwnServices) {
            Stop-OwnService $svc.Name
            & sc.exe delete $svc.Name | Out-Null
            Say "removed service $($svc.Name)"
        }
        $path = Get-MachinePath
        if ($path -contains $Dir) {
            [Environment]::SetEnvironmentVariable('Path', (($path | Where-Object { $_ -ne $Dir }) -join ';'), 'Machine')
            Say "removed $Dir from system PATH"
        }
        # Configs under %ProgramData%\wireguard are kept; only the master/agent role lock is cleared.
        Remove-Item (Join-Path $env:ProgramData 'wireguard\.role') -Force -ErrorAction SilentlyContinue
        Remove-Item (Join-Path $Dir '.role') -Force -ErrorAction SilentlyContinue
    }
}
exit 0
