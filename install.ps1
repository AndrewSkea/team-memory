$ErrorActionPreference = 'Stop'

$RepoOwner = 'AndrewSkea'
$RepoName  = 'team-memory'
$Binary    = 'team-memory-mcp'
$BinDir    = "$env:USERPROFILE\bin"

# ── detect arch ───────────────────────────────────────────────────────────────
$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { Write-Error "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE"; exit 1 }
}

# ── fetch latest release tag ──────────────────────────────────────────────────
Write-Host "Fetching latest release..."
$Release = Invoke-RestMethod "https://api.github.com/repos/$RepoOwner/$RepoName/releases/latest"
$Tag = $Release.tag_name
Write-Host "Installing $Binary $Tag (windows/$Arch)"

# ── download archive + checksums ──────────────────────────────────────────────
$Archive  = "${Binary}_windows_${Arch}.zip"
$Base     = "https://github.com/$RepoOwner/$RepoName/releases/download/$Tag"
$TmpDir   = Join-Path $env:TEMP "team-memory-install-$(Get-Random)"
New-Item -ItemType Directory -Path $TmpDir | Out-Null

try {
    Invoke-WebRequest "$Base/$Archive"      -OutFile "$TmpDir\$Archive"
    Invoke-WebRequest "$Base/checksums.txt" -OutFile "$TmpDir\checksums.txt"

    # ── verify SHA256 ────────────────────────────────────────────────────────
    $Expected = (Get-Content "$TmpDir\checksums.txt" | Where-Object { $_ -match $Archive }) -replace '\s.*', ''
    if (-not $Expected) { Write-Error "$Archive not found in checksums.txt"; exit 1 }
    $Actual = (Get-FileHash "$TmpDir\$Archive" -Algorithm SHA256).Hash.ToLower()
    if ($Expected -ne $Actual) {
        Write-Error "Checksum mismatch!`n  expected: $Expected`n  got:      $Actual"
        exit 1
    }

    # ── install binary ────────────────────────────────────────────────────────
    if (-not (Test-Path $BinDir)) { New-Item -ItemType Directory -Path $BinDir | Out-Null }
    Expand-Archive "$TmpDir\$Archive" -DestinationPath $TmpDir -Force
    # Kill running instance before overwriting the locked binary
    Get-Process -Name $Binary -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 500
    Copy-Item "$TmpDir\$Binary.exe" "$BinDir\$Binary.exe" -Force
    Write-Host "Installed to $BinDir\$Binary.exe"

} finally {
    Remove-Item $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}

# ── add ~/bin to User PATH ────────────────────────────────────────────────────
$UserPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
if ($UserPath -notlike "*$BinDir*") {
    [Environment]::SetEnvironmentVariable('PATH', "$BinDir;$UserPath", 'User')
    Write-Host "Added $BinDir to User PATH"
} else {
    Write-Host "$BinDir already in PATH"
}

# ── read existing config (if any) ────────────────────────────────────────────
$ConfigDir  = "$env:APPDATA\team-memory"
$ConfigFile = "$ConfigDir\config.json"
$ExistingPAT  = ""
$ExistingSlug = ""
if (Test-Path $ConfigFile) {
    try {
        $raw = (Get-Content $ConfigFile -Raw -Encoding UTF8).TrimStart([char]0xFEFF)
        $j   = $raw | ConvertFrom-Json
        $ExistingPAT = $j.token
        if ($j.owner -and $j.repo) { $ExistingSlug = "$($j.owner)/$($j.repo)" }
    } catch {}
}

# ── prompt for config ─────────────────────────────────────────────────────────
Write-Host ""
$PatPrompt = if ($ExistingPAT) { "GitHub PAT [keep existing]" } else { "GitHub PAT (fine-grained, contents:write on your memory repo)" }
$PatSecure = Read-Host $PatPrompt -AsSecureString
$BSTR      = [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($PatSecure)
$PAT       = [System.Runtime.InteropServices.Marshal]::PtrToStringBSTR($BSTR)
[System.Runtime.InteropServices.Marshal]::ZeroFreeBSTR($BSTR)
if (-not $PAT) { $PAT = $ExistingPAT }

$SlugPrompt = if ($ExistingSlug) { "Repo (owner/name) [$ExistingSlug]" } else { "Repo (owner/name, e.g. alice/my-memory)" }
$Slug = Read-Host $SlugPrompt
if (-not $Slug) { $Slug = $ExistingSlug }

# ── write config ──────────────────────────────────────────────────────────────
if ($PAT -and $Slug) {
    $Parts  = $Slug -split '/', 2
    $Owner  = $Parts[0]
    $Repo   = $Parts[1]
    if (-not (Test-Path $ConfigDir)) { New-Item -ItemType Directory -Path $ConfigDir | Out-Null }
    $ConfigJson = @{token=$PAT; owner=$Owner; repo=$Repo; check_first=$false} | ConvertTo-Json
    [System.IO.File]::WriteAllText($ConfigFile, $ConfigJson, (New-Object System.Text.UTF8Encoding $false))
    Write-Host "Config written to $ConfigFile"
} else {
    Write-Host "Warning: PAT or repo blank - skipping config."
    Write-Host "  Write $ConfigFile manually or re-run install.ps1."
}

# ── detect elevation ─────────────────────────────────────────────────────────
$IsAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

# ── choose run mode ───────────────────────────────────────────────────────────
Write-Host ""
Write-Host "How should team-memory-mcp run?"
Write-Host "  1) Always-on service - starts at login, browser at http://127.0.0.1:7438/"
if ($IsAdmin) {
    Write-Host "     (running as admin: also sets up http://team-mem/ shortcut)"
} else {
    Write-Host "     (not admin: uses registry Run key; re-run as admin for http://team-mem/ shortcut)"
}
Write-Host "  2) With Claude Code  - auto-starts when Claude Code runs (lighter weight)"
$RunChoice = Read-Host "Choice [1/2, default 2]"
if (-not $RunChoice) { $RunChoice = "2" }

# ── wire Claude Code hooks ────────────────────────────────────────────────────
$SettingsPath = "$env:USERPROFILE\.claude\settings.json"
$SettingsDir  = Split-Path $SettingsPath
if (-not (Test-Path $SettingsDir)) { New-Item -ItemType Directory -Path $SettingsDir | Out-Null }
if (-not (Test-Path $SettingsPath)) { [System.IO.File]::WriteAllText($SettingsPath, '{}', (New-Object System.Text.UTF8Encoding $false)) }

$HooksPy = @'
import sys, json

path = sys.argv[1]
with open(path, encoding='utf-8') as f:
    settings = json.load(f)

settings.setdefault('hooks', {})
new_hooks = {
    'Stop':       [{'matcher': '', 'hooks': [{'type': 'command', 'command': 'team-memory-mcp --once session-end', 'timeout': 60}]}],
    'PreCompact': [{'matcher': '', 'hooks': [{'type': 'command', 'command': 'team-memory-mcp --once precompact',  'timeout': 60}]}],
}
for event, entries in new_hooks.items():
    existing = settings['hooks'].get(event, [])
    already = any(
        h.get('command', '').find('team-memory-mcp') >= 0
        for e in existing for h in e.get('hooks', [])
    )
    if not already:
        settings['hooks'][event] = existing + entries
        print(f'  Hooked {event}')
    else:
        print(f'  {event} already wired')

with open(path, 'w', encoding='utf-8') as f:
    json.dump(settings, f, indent=2)
'@
$HooksPy | python - $SettingsPath

# ── register MCP server in ~/.claude.json ────────────────────────────────────
$ClaudeJson = "$env:USERPROFILE\.claude.json"
if (-not (Test-Path $ClaudeJson)) { [System.IO.File]::WriteAllText($ClaudeJson, '{}', (New-Object System.Text.UTF8Encoding $false)) }

# Service mode: Claude Code starts on port 7439 to avoid conflict with service on 7438
# Pass args as individual positional params (PS 5.1 strips quotes from JSON strings)
if ($RunChoice -eq "1") {
    $McpArgList = @("--mcp", "--port", "7439")
} else {
    $McpArgList = @("--mcp")
}

$McpPy = @'
import sys, json

path = sys.argv[1]
binary_path = sys.argv[2]
args = sys.argv[3:]
with open(path, encoding='utf-8') as f:
    data = json.load(f)

data.setdefault('mcpServers', {})
if 'team-memory' not in data['mcpServers']:
    data['mcpServers']['team-memory'] = {'command': binary_path, 'args': args}
    print('  MCP server registered')
else:
    data['mcpServers']['team-memory']['args'] = args
    print('  MCP server updated')

with open(path, 'w', encoding='utf-8') as f:
    json.dump(data, f, indent=2)
'@
$McpPy | python - $ClaudeJson "$BinDir\$Binary.exe" @McpArgList

# ── service setup ─────────────────────────────────────────────────────────────
$Port = 7438
$WebUrl = "http://127.0.0.1:$Port/"

if ($RunChoice -eq "1") {
    $ExePath = "$BinDir\$Binary.exe"

    if ($IsAdmin) {
        # ── admin path: Task Scheduler (RunLevel Highest) + hosts + portproxy 80->7438 ──
        # Go uses raw sockets (net.Listen), not HTTP.sys, so binding port 80 directly
        # requires the process to be elevated. Using portproxy avoids that: the binary
        # always listens on 7438 and netsh forwards 127.0.0.1:80 -> 127.0.0.1:7438.
        $HostsFile = "$env:SystemRoot\System32\drivers\etc\hosts"
        $HostsContent = Get-Content $HostsFile -Raw -ErrorAction SilentlyContinue
        if ($HostsContent -notlike "*team-mem*") {
            Add-Content -Path $HostsFile -Value "`r`n127.0.0.1 team-mem" -ErrorAction SilentlyContinue
            Write-Host "  Added team-mem to hosts file"
        } else {
            Write-Host "  team-mem already in hosts file"
        }

        # portproxy: browser hits team-mem:80 -> 127.0.0.1:7438
        & netsh interface portproxy add v4tov4 listenport=80 listenaddress=127.0.0.1 connectport=7438 connectaddress=127.0.0.1 | Out-Null
        if ($LASTEXITCODE -eq 0) {
            Write-Host "  Port 80 portproxy set - browser URL: http://team-mem/"
            $WebUrl = "http://team-mem/"
        } else {
            Write-Host "  portproxy failed - browser URL: http://team-mem:7438/"
            $WebUrl = "http://team-mem:7438/"
        }

        $TaskName  = "TeamMemoryMCP"
        $Action    = New-ScheduledTaskAction -Execute $ExePath -Argument "--port 7438"
        $Trigger   = New-ScheduledTaskTrigger -AtLogOn
        $Settings  = New-ScheduledTaskSettingsSet -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit 0 -MultipleInstances IgnoreNew
        $Principal = New-ScheduledTaskPrincipal -UserId $env:USERNAME -RunLevel Highest
        Register-ScheduledTask -TaskName $TaskName -Action $Action -Trigger $Trigger -Settings $Settings -Principal $Principal -Force | Out-Null
        # Kill any existing instance so the task can start cleanly
        Get-Process -Name $Binary -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
        Start-Sleep -Milliseconds 500
        try {
            Start-ScheduledTask -TaskName $TaskName -ErrorAction Stop
            Start-Sleep -Milliseconds 1000
            $taskState = (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue).State
            Write-Host "  Task '$TaskName' registered, state: $taskState"
        } catch {
            # Fallback: start directly (task will still auto-start at next logon)
            Start-Process -FilePath $ExePath -ArgumentList '--port 7438' -WindowStyle Hidden
            Write-Host "  Task '$TaskName' registered (started directly as fallback)"
        }

    } else {
        # ── non-admin path: registry Run key, port 7438 ───────────────────────
        $RegPath = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
        Set-ItemProperty -Path $RegPath -Name 'TeamMemoryMCP' -Value "`"$ExePath`" --port 7438" -ErrorAction Stop
        Write-Host "  Autostart registered in HKCU Run key"

        # Start now in background (don't wait)
        Start-Process -FilePath $ExePath -ArgumentList '--port 7438' -WindowStyle Hidden
        Write-Host "  Started team-memory-mcp on port 7438"

        Write-Host ""
        Write-Host "  NOTE: http://team-mem/ shortcut requires admin. To set it up:"
        Write-Host "    1. Re-run this script as Administrator"
        Write-Host "    OR manually add to $env:SystemRoot\System32\drivers\etc\hosts:"
        Write-Host "       127.0.0.1 team-mem"
        Write-Host "    (then access via http://team-mem:7438/)"

        $WebUrl = "http://127.0.0.1:7438/"
    }
} else {
    $WebUrl = "http://127.0.0.1:7438/  (available while Claude Code is running)"
}

# ── summary ───────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "team-memory-mcp installed OK"
Write-Host ""
Write-Host "  Binary:  $BinDir\$Binary.exe"
Write-Host "  Config:  $env:APPDATA\team-memory\config.json"
Write-Host "  Hooks:   $env:USERPROFILE\.claude\settings.json  (Stop, PreCompact)"
Write-Host "  MCP:     $env:USERPROFILE\.claude.json           (team-memory server)"
Write-Host "  Web UI:  $WebUrl"
Write-Host ""
Write-Host "  Restart your terminal for PATH changes to take effect."
