# Concord's one-line installer, for Windows (PowerShell 5.1 or 7):
#
#   irm https://github.com/JMThomas00/Concord/releases/latest/download/install.ps1 | iex
#
# It fetches concord-install (cmd/install) from the latest release, checks
# it against the release's SHA256SUMS, and runs it: a guided form that
# installs the client, server and/or hub, with everything they need.
# $env:CONCORD_RELEASE picks another release; $env:CONCORD_INSTALL_ARGS
# passes arguments (e.g. "--dry-run").
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue' # Windows PowerShell's progress bar slows downloads to a crawl
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$repo = 'JMThomas00/Concord'
$base = if ($env:CONCORD_RELEASE) { "https://github.com/$repo/releases/download/$($env:CONCORD_RELEASE)" } else { "https://github.com/$repo/releases/latest/download" }
$asset = 'concord-install-windows-amd64.exe' # ARM64 Windows runs it too

Write-Host "Fetching the Concord installer..." -ForegroundColor Magenta
$dir = Join-Path ([IO.Path]::GetTempPath()) 'concord-install'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$exe = Join-Path $dir $asset
try {
    Invoke-WebRequest -Uri "$base/$asset" -OutFile $exe -UseBasicParsing
} catch {
    Write-Host "Couldn't download $asset (is there a published release yet? https://github.com/$repo/releases)" -ForegroundColor Red
    return
}

try {
    $sums = (Invoke-WebRequest -Uri "$base/SHA256SUMS" -UseBasicParsing).Content
    if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) }
    $line = $sums -split "`n" | Where-Object { $_ -match ("\s\*?" + [regex]::Escape($asset) + "\s*$") } | Select-Object -First 1
    if ($line) {
        $want = ($line.Trim() -split '\s+')[0]
        $got = (Get-FileHash -Path $exe -Algorithm SHA256).Hash
        if ($got -ne $want.ToUpper()) {
            Write-Host "The installer doesn't match its published checksum, so it wasn't run. Try again in a minute." -ForegroundColor Red
            Remove-Item $exe -Force
            return
        }
    }
} catch { } # no SHA256SUMS: https alone

$passArgs = @()
if ($env:CONCORD_INSTALL_ARGS) { $passArgs += $env:CONCORD_INSTALL_ARGS -split '\s+' }
$passArgs += $args
& $exe @passArgs
