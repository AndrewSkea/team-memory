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
    Write-Host "Warning: PAT or repo blank — skipping config."
    Write-Host "  Write $ConfigFile manually or re-run install.ps1."
}

# ── wire Claude Code hooks ────────────────────────────────────────────────────
$SettingsPath = "$env:USERPROFILE\.claude\settings.json"
$SettingsDir  = Split-Path $SettingsPath
if (-not (Test-Path $SettingsDir)) { New-Item -ItemType Directory -Path $SettingsDir | Out-Null }
if (-not (Test-Path $SettingsPath)) { '{}' | Set-Content $SettingsPath -Encoding UTF8 }

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
if (-not (Test-Path $ClaudeJson)) { '{}' | Set-Content $ClaudeJson -Encoding UTF8 }

$McpPy = @'
import sys, json

path = sys.argv[1]
binary_path = sys.argv[2]
with open(path, encoding='utf-8') as f:
    data = json.load(f)

data.setdefault('mcpServers', {})
if 'team-memory' not in data['mcpServers']:
    data['mcpServers']['team-memory'] = {'command': binary_path, 'args': ['--mcp']}
    print('  MCP server registered')
else:
    print('  MCP server already registered')

with open(path, 'w', encoding='utf-8') as f:
    json.dump(data, f, indent=2)
'@
$McpPy | python - $ClaudeJson "$BinDir\$Binary.exe"

# ── summary ───────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "✓ team-memory-mcp installed"
Write-Host ""
Write-Host "  Binary:  $BinDir\$Binary.exe"
Write-Host "  Config:  $env:APPDATA\team-memory\config.json"
Write-Host "  Hooks:   $env:USERPROFILE\.claude\settings.json  (Stop, PreCompact)"
Write-Host "  MCP:     $env:USERPROFILE\.claude.json           (team-memory server)"
Write-Host ""
Write-Host "  Restart your terminal for PATH changes to take effect."
if ($Slug) {
    Write-Host "  Start a Claude Code session — it will auto-save to $Slug when you stop."
}
