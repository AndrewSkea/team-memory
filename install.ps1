# team-memory installer for Windows.
#
#   irm https://raw.githubusercontent.com/AndrewSkea/team-memory/master/install.ps1 | iex
#
# Environment: TEAM_MEMORY_VERSION (e.g. v1.0.0, default latest),
#              TEAM_MEMORY_BIN_DIR (default %USERPROFILE%\bin)
$ErrorActionPreference = 'Stop'

$Repo    = 'AndrewSkea/team-memory'
$Binary  = 'team-memory-mcp'
$BinDir  = if ($env:TEAM_MEMORY_BIN_DIR) { $env:TEAM_MEMORY_BIN_DIR } else { "$env:USERPROFILE\bin" }
$Version = if ($env:TEAM_MEMORY_VERSION) { $env:TEAM_MEMORY_VERSION } else { 'latest' }
$ExePath = Join-Path $BinDir "$Binary.exe"

$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'amd64' }  # runs under x64 emulation
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$Archive = "${Binary}_windows_${Arch}.zip"
$Base = if ($env:TEAM_MEMORY_DOWNLOAD_BASE) { $env:TEAM_MEMORY_DOWNLOAD_BASE } elseif ($Version -eq 'latest') { "https://github.com/$Repo/releases/latest/download" } else { "https://github.com/$Repo/releases/download/$Version" }
$Tmp = Join-Path ([System.IO.Path]::GetTempPath()) "team-memory-install-$([guid]::NewGuid())"
New-Item -ItemType Directory -Path $Tmp | Out-Null

try {
    Write-Host "Downloading $Binary ($Version, windows/$Arch)..."
    $ProgressPreference = 'SilentlyContinue'
    Invoke-WebRequest "$Base/$Archive" -OutFile "$Tmp\$Archive" -UseBasicParsing
    Invoke-WebRequest "$Base/checksums.txt" -OutFile "$Tmp\checksums.txt" -UseBasicParsing

    # ── verify SHA-256 ───────────────────────────────────────────────────────
    $Line = Get-Content "$Tmp\checksums.txt" | Where-Object { ($_ -split '\s+')[1] -eq $Archive } | Select-Object -First 1
    if (-not $Line) { throw "$Archive not listed in checksums.txt" }
    $Expected = ($Line -split '\s+')[0].ToLower()
    $Actual = (Get-FileHash "$Tmp\$Archive" -Algorithm SHA256).Hash.ToLower()
    if ($Expected -ne $Actual) { throw "Checksum mismatch (expected $Expected, got $Actual)" }

    # ── install binary ───────────────────────────────────────────────────────
    Expand-Archive "$Tmp\$Archive" -DestinationPath $Tmp -Force
    if (-not (Test-Path $BinDir)) { New-Item -ItemType Directory -Path $BinDir | Out-Null }
    # A running copy (web UI / MCP server) locks the exe; stop it first.
    Get-Process -Name $Binary -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 500
    Copy-Item "$Tmp\$Binary.exe" $ExePath -Force
    Write-Host "Installed $ExePath ($(& $ExePath version))"
} finally {
    Remove-Item $Tmp -Recurse -Force -ErrorAction SilentlyContinue
}

# ── put BinDir on the user PATH ──────────────────────────────────────────────
$UserPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
if (($UserPath -split ';') -notcontains $BinDir) {
    [Environment]::SetEnvironmentVariable('PATH', "$BinDir;$UserPath", 'User')
    $env:PATH = "$BinDir;$env:PATH"
    Write-Host "Added $BinDir to your PATH (new terminals will pick it up)"
}

# ── configure: memory repo, Claude Code hooks, MCP server ────────────────────
& $ExePath install --no-service @args
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

# ── optional: keep the web UI running in the background ──────────────────────
$RunKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$HasAutostart = $null -ne (Get-ItemProperty -Path $RunKey -Name 'TeamMemory' -ErrorAction SilentlyContinue)
$Answer = if ($HasAutostart) { 'y' } else { Read-Host "Keep the web UI running in the background (starts at login)? [y/N]" }
if ($Answer -match '^(y|yes)$') {
    Set-ItemProperty -Path $RunKey -Name 'TeamMemory' -Value "`"$ExePath`" serve --port 7438"
    # Clean up the pre-1.0 autostart entry name.
    Remove-ItemProperty -Path $RunKey -Name 'TeamMemoryMCP' -ErrorAction SilentlyContinue
    Start-Process -FilePath $ExePath -ArgumentList 'serve','--port','7438' -WindowStyle Hidden
    Write-Host "Background web UI enabled: http://127.0.0.1:7438/"
}
