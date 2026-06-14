$ErrorActionPreference = "Stop"

$repo = Split-Path -Parent $PSScriptRoot
$exe = Join-Path $repo "synthos-desktop-agent.exe"

Set-Location $repo

if (!(Test-Path $exe)) {
  Write-Host "Building SYNTHOS desktop agent..."
  go build -o $exe ./cmd/desktopagent
}

Write-Host "Starting SYNTHOS desktop agent..."
Start-Process -FilePath $exe -WorkingDirectory $repo -WindowStyle Hidden
Start-Sleep -Seconds 2
Start-Process "http://127.0.0.1:8788"

Write-Host "SYNTHOS desktop agent is running at http://127.0.0.1:8788"
