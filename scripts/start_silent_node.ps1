$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $repo "synthos-silent-node.exe"

Set-Location $repo

if (!(Test-Path $exe)) {
  Write-Host "Building SYNTHOS silent node..."
  go build -o $exe ./cmd/silentnode
}

if (!$env:SYNTHOS_RELAY_URLS -and !$env:SYNTHOS_REGISTRY_URL) {
  $env:SYNTHOS_RELAY_URLS = "https://synthos-peer-registry.jamesishamwilliams.workers.dev"
}

Write-Host "Starting SYNTHOS silent node. No ports will be opened."
Write-Host "Relay set: $($env:SYNTHOS_RELAY_URLS)"
Start-Process -FilePath $exe -WorkingDirectory $repo -WindowStyle Hidden
Write-Host "Started outbound-only SYNTHOS node."
