param(
  [string]$OutFile = "SYNTHOS_AUDIT_PACKET.zip"
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$root = Split-Path -Parent $root

Set-Location $root

if (Test-Path $OutFile) {
  Remove-Item -Force $OutFile
}

$include = @(
  "go.mod",
  "README.md",
  "cmd",
  "internal",
  "docs"
)

$tempDir = Join-Path $env:TEMP ("synthos-audit-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $tempDir | Out-Null

foreach ($p in $include) {
  if (Test-Path $p) {
    Copy-Item -Recurse -Force $p (Join-Path $tempDir $p)
  }
}

Compress-Archive -Path (Join-Path $tempDir "*") -DestinationPath $OutFile -Force
Remove-Item -Recurse -Force $tempDir

Write-Host "Wrote $OutFile"

